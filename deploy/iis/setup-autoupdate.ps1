<#
.SYNOPSIS
  One-time setup of signed auto-updates for APIMonitory.
.DESCRIPTION
  Run ONCE, in an elevated Windows PowerShell 5.1, on the server
  (safe to re-run). It installs pull-release.ps1 and a scheduled
  task that checks the latest GitHub release every 2 minutes and
  installs api.exe only if it is signed by the deploy key whose
  PUBLIC half you pass in -PublicKey. Needs outbound HTTPS to
  github.com and OpenSSH's ssh-keygen (8.1 or newer).

  It creates <Root>\state and <Root>\incoming, writes
  <Root>\allowed_signers (the only trusted key) and copies
  pull-release.ps1 to <Root>. Because the task runs as SYSTEM,
  those items, plus <Root>\deploy and <Root>\keepalive.ps1 (which
  SYSTEM tasks also run), are locked to SYSTEM + Administrators with
  no inheritance: nobody else can change what they execute or
  trust. <Root> and <Root>\web are left alone (the app pool needs
  them); the script only warns if others can write to <Root>.
.EXAMPLE
  # After copying ~/.ssh/apimonitory_deploy.pub on the Mac:
  .\setup-autoupdate.ps1 -PublicKey (Get-Clipboard)
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$PublicKey,
    [string]$Repo = 'maumonteros88/APIMonitory',
    [string]$HostName = 'apimonitory.dev.goiar.com',
    [string]$Root = 'C:\inetpub\APIMonitory',
    [string]$SshKeygen = 'C:\Program Files\OpenSSH\ssh-keygen.exe'
)

$ErrorActionPreference = 'Stop'

$Root = $Root.TrimEnd('\')
if ($Root.Length -lt 4) {
    throw "-Root looks wrong: '$Root'."
}

function Invoke-Native {
    # Runs an exe without PowerShell turning stderr into errors.
    param([string]$Exe, [string[]]$Arguments)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & $Exe @Arguments 2>&1
        $code = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $prev
    }
    return [pscustomobject]@{
        Code   = $code
        Output = ($out | Out-String).Trim()
    }
}

function Lock-Path {
    # SYSTEM + Administrators only, inheritance removed. For a
    # folder the reset is recursive, so existing children are reset
    # too and inherit the locked ACL.
    param([string]$Path, [bool]$IsDir)
    $full = 'F'
    $reset = @($Path, '/reset')
    if ($IsDir) {
        $full = '(OI)(CI)F'
        $reset = @($Path, '/reset', '/T')
    }
    $steps = @(
        $reset,
        @($Path, '/inheritance:r'),
        @($Path, '/grant:r', "*S-1-5-18:$full"),
        @($Path, '/grant:r', "*S-1-5-32-544:$full")
    )
    foreach ($step in $steps) {
        $r = Invoke-Native -Exe 'icacls.exe' -Arguments $step
        if ($r.Code -ne 0) {
            throw "icacls failed on $Path : $($r.Output)"
        }
    }
}

# --- checks (before changing anything) ----------------------------
$winId = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($winId)
$adminRole = [Security.Principal.WindowsBuiltInRole]::Administrator
if (-not $principal.IsInRole($adminRole)) {
    throw 'Run this script in an elevated PowerShell.'
}

$key = ($PublicKey -join ' ').Trim()
if ($key -match '[\x00-\x1F\x7F]') {
    throw 'PublicKey has control characters (use one single line).'
}
$keyRx = '^ssh-ed25519 [A-Za-z0-9+/=]+( \S.*)?$'
if ($key -notmatch $keyRx) {
    throw 'PublicKey must be one ssh-ed25519 public key line.'
}
$keyBlob = ($key -split ' ')[1]

$repoRx = '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$'
if ($Repo -notmatch $repoRx) {
    throw "-Repo must look like owner/name, got '$Repo'."
}
if ($HostName -notmatch '^[A-Za-z0-9.-]+$') {
    throw "-HostName looks invalid: '$HostName'."
}

if (-not (Test-Path $SshKeygen)) {
    throw "ssh-keygen not found at $SshKeygen."
}
$sshExe = Join-Path (Split-Path $SshKeygen -Parent) 'ssh.exe'
if (-not (Test-Path $sshExe)) {
    throw "ssh.exe not found next to ssh-keygen ($sshExe)."
}
$ver = Invoke-Native -Exe $sshExe -Arguments @('-V')
if (-not ($ver.Output -match '_(\d+)\.(\d+)')) {
    throw "Cannot read the OpenSSH version: $($ver.Output)"
}
$major = [int]$Matches[1]
$minor = [int]$Matches[2]
if (($major * 100 + $minor) -lt 801) {
    throw "OpenSSH $major.$minor is too old: -Y verify needs 8.1+."
}

$webDir = Join-Path $Root 'web'
$deployDir = Join-Path $Root 'deploy'
foreach ($path in @($webDir, $deployDir)) {
    if (-not (Test-Path $path)) {
        throw "Missing $path. Run setup-iis.ps1 first."
    }
}
foreach ($name in @('update.ps1', 'keepalive.ps1')) {
    if (-not (Test-Path (Join-Path $deployDir $name))) {
        throw "Missing $name in $deployDir."
    }
}
$pullSource = Join-Path $PSScriptRoot 'pull-release.ps1'
if (-not (Test-Path $pullSource)) {
    throw 'pull-release.ps1 not found next to this script.'
}

# --- Folders, trusted key, script ---------------------------------
$stateDir = Join-Path $Root 'state'
$incomingDir = Join-Path $Root 'incoming'
$signers = Join-Path $Root 'allowed_signers'
$pullScript = Join-Path $Root 'pull-release.ps1'
foreach ($dir in @($stateDir, $incomingDir)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir | Out-Null
    }
}
$nl = [string][char]10
$ident = 'apimonitory-deploy'
$line = $ident + ' namespaces="' + $ident + '"'
$line += ' ssh-ed25519 ' + $keyBlob
[IO.File]::WriteAllText($signers, $line + $nl)
Copy-Item -Path $pullSource -Destination $pullScript -Force

Lock-Path -Path $signers -IsDir $false
Lock-Path -Path $pullScript -IsDir $false
Lock-Path -Path $stateDir -IsDir $true
Lock-Path -Path $incomingDir -IsDir $true

# SYSTEM also runs deploy\update.ps1 and keepalive.ps1 (the existing
# keep-alive task runs the copy in <Root>), so nobody else may edit
# them. <Root> and <Root>\web are NOT locked: the app pool needs
# Read+Execute on web and Modify on web\logs.
Lock-Path -Path $deployDir -IsDir $true
$kaScript = Join-Path $Root 'keepalive.ps1'
if (Test-Path $kaScript) {
    Lock-Path -Path $kaScript -IsDir $false
}
else {
    Write-Warning "$kaScript not found (run setup-iis.ps1)."
}

# Warn if others can add or replace files directly in <Root>
# (that would let them swap what SYSTEM runs there). Write covers
# WriteData/AppendData/attributes, and every Modify or FullControl
# ACE contains those bits, so one mask catches them all.
$weak = @('Everyone', 'BUILTIN\Users', 'IIS_IUSRS',
    'NT AUTHORITY\Authenticated Users')
$rights = [Security.AccessControl.FileSystemRights]
$writeMask = $rights::Write -bor $rights::DeleteSubdirectoriesAndFiles
$writeMask = $writeMask -bor $rights::ChangePermissions
$writeMask = $writeMask -bor $rights::TakeOwnership
foreach ($ace in (Get-Acl -Path $Root).Access) {
    $isAllow = $ace.AccessControlType -eq 'Allow'
    $canWrite = ($ace.FileSystemRights -band $writeMask) -ne 0
    $who = $ace.IdentityReference.Value
    $isWeak = $weak -contains $who -or $who -like '*\IIS_IUSRS'
    if ($isAllow -and $canWrite -and $isWeak) {
        Write-Warning "$who can write to $Root; restrict it"
        Write-Warning 'to administrators (keep web\ as it is).'
    }
}

# --- Scheduled task (SYSTEM, every 2 min, indefinitely) -----------
$taskName = 'APIMonitory auto-update'
$psOpts = '-NoProfile -NonInteractive -ExecutionPolicy Bypass'
$psFile = '-File "{0}"' -f $pullScript
$psArgs = "$psOpts $psFile -Repo $Repo"
$psArgs += ' -Root "' + $Root + '" -HostName ' + $HostName
$psArgs += ' -SshKeygen "' + $SshKeygen + '"'
$action = @{
    Execute  = 'powershell.exe'
    Argument = $psArgs
}
$trigger = @{
    Once               = $true
    At                 = (Get-Date)
    RepetitionInterval = (New-TimeSpan -Minutes 2)
}
$taskPrincipal = @{
    UserId    = 'SYSTEM'
    LogonType = 'ServiceAccount'
    RunLevel  = 'Highest'
}
$settings = @{
    ExecutionTimeLimit = (New-TimeSpan -Minutes 10)
    MultipleInstances  = 'IgnoreNew'
    StartWhenAvailable = $true
}
$register = @{
    TaskName  = $taskName
    Action    = New-ScheduledTaskAction @action
    Trigger   = New-ScheduledTaskTrigger @trigger
    Principal = New-ScheduledTaskPrincipal @taskPrincipal
    Settings  = New-ScheduledTaskSettingsSet @settings
    Force     = $true
}
Register-ScheduledTask @register | Out-Null

# --- Summary ------------------------------------------------------
$logFile = Join-Path $Root 'autoupdate.log'
Write-Host ''
Write-Host '--- APIMonitory auto-update setup ---'
Write-Host "Repo          : https://github.com/$Repo"
Write-Host "OpenSSH       : $major.$minor ($SshKeygen)"
Write-Host "Trusted key   : $signers (ssh-ed25519)"
Write-Host "Script        : $pullScript"
Write-Host "State / temp  : $stateDir, $incomingDir"
Write-Host "Task          : '$taskName' every 2 min as SYSTEM"
Write-Host "Log           : $logFile"
Write-Host ''
$q = '"' + $taskName + '"'
Write-Host "Check   : Get-ScheduledTaskInfo -TaskName $q"
Write-Host "Run now : Start-ScheduledTask -TaskName $q"
