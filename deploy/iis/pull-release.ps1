<#
.SYNOPSIS
  Installs the latest signed APIMonitory release from GitHub.
.DESCRIPTION
  Meant to run every couple of minutes as a scheduled task (SYSTEM),
  registered by setup-autoupdate.ps1. Each run:
    1. downloads version.txt of the latest GitHub release (a plain
       file download, no API and no token; the repo is public)
    2. exits quietly if that version is already in
       <Root>\state\deployed.txt or <Root>\state\failed.txt
    3. downloads api.exe and api.exe.sig of that same release into
       <Root>\incoming
    4. verifies the signature with ssh-keygen -Y verify against
       <Root>\allowed_signers (only the deploy key is trusted)
    5. runs deploy\update.ps1 on a valid signature and records the
       version in deployed.txt; a bad signature or a failed update
       records it in failed.txt so it is never retried in a loop
  Everything is logged to <Root>\autoupdate.log (last ~500 lines).
  To retry a version marked as failed, publish a new release or
  delete <Root>\state\failed.txt.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$')]
    [string]$Repo,
    [string]$Root = 'C:\inetpub\APIMonitory',
    [string]$HostName = 'apimonitory.dev.goiar.com',
    [string]$SshKeygen = 'C:\Program Files\OpenSSH\ssh-keygen.exe'
)

$ErrorActionPreference = 'Stop'
$tls12 = [Net.SecurityProtocolType]::Tls12
[Net.ServicePointManager]::SecurityProtocol = $tls12

$ident = 'apimonitory-deploy'
$base = "https://github.com/$Repo/releases"
$stateDir = Join-Path $Root 'state'
$incoming = Join-Path $Root 'incoming'
$logFile = Join-Path $Root 'autoupdate.log'
$signers = Join-Path $Root 'allowed_signers'
$updateScript = Join-Path $Root 'deploy\update.ps1'
$deployedFile = Join-Path $stateDir 'deployed.txt'
$failedFile = Join-Path $stateDir 'failed.txt'
$exePath = Join-Path $incoming 'api.exe'
$sigPath = Join-Path $incoming 'api.exe.sig'
$rollbackPath = Join-Path $incoming 'rollback.exe'

function Short {
    # One line, at most 400 chars, for the log.
    param([string]$Text)
    $one = ($Text -replace '\s+', ' ').Trim()
    if ($one.Length -gt 400) { $one = $one.Substring(0, 400) }
    return $one
}

function Write-Log {
    param([string]$Message)
    $stamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
    Add-Content -Path $logFile -Value "$stamp $Message"
}

function Limit-Log {
    if (-not (Test-Path $logFile)) { return }
    $all = @(Get-Content -Path $logFile)
    if ($all.Count -gt 500) {
        $all | Select-Object -Last 500 | Set-Content -Path $logFile
    }
}

function Read-State {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return '' }
    return ([IO.File]::ReadAllText($Path)).Trim()
}

function New-Client {
    $wc = New-Object Net.WebClient
    $wc.Headers.Add('User-Agent', 'APIMonitory-autoupdate')
    $level = [Net.Cache.RequestCacheLevel]::BypassCache
    $wc.CachePolicy = New-Object Net.Cache.RequestCachePolicy($level)
    return $wc
}

function Get-Remote {
    param([string]$Url)
    $wc = New-Client
    try { return $wc.DownloadString($Url) }
    finally { $wc.Dispose() }
}

function Save-Remote {
    param([string]$Url, [string]$Path)
    $wc = New-Client
    try { $wc.DownloadFile($Url, $Path) }
    finally { $wc.Dispose() }
}

function Test-Signature {
    # Windows PowerShell 5.1 pipelines corrupt binary data, so the
    # exe is streamed to ssh-keygen's stdin as raw bytes.
    param([string]$Exe, [string]$Sig)
    $argText = '-Y verify -f "' + $signers + '" -I ' + $ident
    $argText += ' -n ' + $ident + ' -s "' + $Sig + '"'
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = $SshKeygen
    $psi.Arguments = $argText
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $proc = [Diagnostics.Process]::Start($psi)
    try {
        $outTask = $proc.StandardOutput.ReadToEndAsync()
        $errTask = $proc.StandardError.ReadToEndAsync()
        $stdin = $proc.StandardInput.BaseStream
        $fs = [IO.File]::OpenRead($Exe)
        try {
            $fs.CopyTo($stdin)
        }
        catch [IO.IOException] {
            # ssh-keygen quit early; its exit code tells why.
            Write-Verbose 'stdin closed early'
        }
        finally {
            $fs.Dispose()
        }
        try { $stdin.Close() } catch [IO.IOException] { }
        if (-not $proc.WaitForExit(60000)) {
            $proc.Kill()
            return [pscustomobject]@{
                Ok = $false; Text = 'ssh-keygen timed out'
            }
        }
        $proc.WaitForExit()
        $text = $outTask.Result + ' ' + $errTask.Result
        return [pscustomobject]@{
            Ok   = ($proc.ExitCode -eq 0)
            Text = "exit $($proc.ExitCode): $(Short $text)"
        }
    }
    finally {
        $proc.Dispose()
    }
}

function Invoke-Update {
    # Runs update.ps1 and returns '' on success or the error text.
    # Output is collected as it arrives, so it is kept on a throw.
    param([string]$Source)
    $lines = New-Object 'System.Collections.Generic.List[string]'
    $err = ''
    $upd = @{
        SourceExe    = $Source
        HostName     = $HostName
        PhysicalPath = (Join-Path $Root 'web')
    }
    try {
        & $updateScript @upd *>&1 |
            ForEach-Object { $lines.Add([string]$_) }
    }
    catch {
        $err = Short $_.Exception.Message
        if (-not $err) { $err = 'update.ps1 failed' }
    }
    Write-Log "update.ps1: $(Short ($lines -join ' '))"
    return $err
}

function Invoke-Rollback {
    # update.ps1 swaps the exe and only then warms the site up. If
    # it failed, restore the previous exe only when the swap happened
    # (live exe == the verified new one).
    $live = Join-Path $Root 'web\api.exe'
    $bak = Join-Path $Root 'web\api.exe.bak'
    try {
        $liveHash = (Get-FileHash -Path $live).Hash
        $newHash = (Get-FileHash -Path $exePath).Hash
    }
    catch {
        $why = Short $_.Exception.Message
        Write-Log "ROLLBACK FAILED: cannot compare exes: $why"
        Write-ManualHelp
        return
    }
    if ($liveHash -ne $newHash) {
        Write-Log 'Swap did not happen, no rollback needed.'
        return
    }
    if (-not (Test-Path $bak)) {
        Write-Log "ROLLBACK FAILED: $bak is missing."
        Write-ManualHelp
        return
    }
    Write-Log 'New exe is live but failed; restoring the previous.'
    try {
        Copy-Item -Path $bak -Destination $rollbackPath -Force
    }
    catch {
        $why = Short $_.Exception.Message
        Write-Log "ROLLBACK FAILED: cannot copy .bak: $why"
        Write-ManualHelp
        return
    }
    $err = Invoke-Update -Source $rollbackPath
    if ($err) {
        Write-Log "ROLLBACK FAILED: $err"
        Write-ManualHelp
        return
    }
    Write-Log 'ROLLED BACK to previous exe.'
}

function Write-ManualHelp {
    Write-Log '  Manual: copy web\api.exe.bak to another path,'
    Write-Log '  then run deploy\update.ps1 -SourceExe <that copy>'
    Write-Log "  -HostName $HostName (not api.exe.bak itself)."
}

function Remove-Incoming {
    $files = @($exePath, $sigPath, $rollbackPath)
    Remove-Item $files -Force -ErrorAction SilentlyContinue
}

function Invoke-Pull {
    try {
        $raw = Get-Remote "$base/latest/download/version.txt"
    }
    catch {
        $why = Short $_.Exception.Message
        Write-Log "Cannot read version.txt: $why"
        return 1
    }
    $version = $raw.Trim([char]0xFEFF).Trim()
    $okRx = '^[A-Za-z0-9._-]+( [A-Za-z0-9._-]+)?$'
    if ($version.Length -gt 120 -or $version -notmatch $okRx) {
        Write-Log "Ignoring odd version.txt: $(Short $raw)"
        return 1
    }
    if ($version -eq (Read-State $deployedFile)) { return 0 }
    if ($version -eq (Read-State $failedFile)) { return 0 }

    Write-Log "New release: $version"
    $parts = $version -split ' '
    $dl = "$base/latest/download"
    if ($parts.Count -eq 2) { $dl = "$base/download/$($parts[1])" }

    if (-not (Test-Path $SshKeygen)) {
        Write-Log "ssh-keygen not found: $SshKeygen"
        return 1
    }
    if (-not (Test-Path $signers)) {
        Write-Log "Missing $signers. Run setup-autoupdate.ps1."
        return 1
    }
    if (-not (Test-Path $incoming)) {
        New-Item -ItemType Directory -Path $incoming | Out-Null
    }
    Remove-Incoming
    try {
        try {
            Save-Remote "$dl/api.exe" $exePath
            Save-Remote "$dl/api.exe.sig" $sigPath
        }
        catch {
            # Network problem: not the release's fault, retry later.
            $why = Short $_.Exception.Message
            Write-Log "Download failed (will retry): $why"
            return 1
        }
        $check = Test-Signature -Exe $exePath -Sig $sigPath
        if (-not $check.Ok) {
            Write-Log "BAD SIGNATURE for $version, not installed."
            Write-Log "  $($check.Text)"
            Set-Content -Path $failedFile -Value $version
            return 1
        }
        Write-Log "Signature OK for $version, updating."
        $err = Invoke-Update -Source $exePath
        if ($err) {
            Write-Log "UPDATE FAILED for $version, not retrying."
            Write-Log "  $err"
            Set-Content -Path $failedFile -Value $version
            Invoke-Rollback
            return 1
        }
        Set-Content -Path $deployedFile -Value $version
        Remove-Item $failedFile -Force -ErrorAction SilentlyContinue
        Write-Log "Deployed $version."
        return 0
    }
    finally {
        Remove-Incoming
    }
}

$code = 1
try {
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir | Out-Null
    }
    $code = Invoke-Pull
}
catch {
    Write-Log "Unexpected error: $(Short $_.Exception.Message)"
}
Limit-Log
exit $code
