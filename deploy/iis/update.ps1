<#
.SYNOPSIS
  Replaces api.exe on the APIMonitory IIS site.
.DESCRIPTION
  Takes the site offline with app_offline.htm (ANCM then stops
  api.exe), waits until the exe is unlocked, keeps the current one
  as api.exe.bak, swaps it, brings the site back and warms it up
  with keepalive.ps1 (ANCM only relaunches api.exe on a request).
  If the swap fails, the old exe stays in place.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SourceExe,
    [Parameter(Mandatory = $true)]
    [string]$HostName,
    [string]$PhysicalPath = 'C:\inetpub\APIMonitory\web',
    [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = 'Stop'

$target = Join-Path $PhysicalPath 'api.exe'
$backup = "$target.bak"
$staged = "$target.new"
$webConfig = Join-Path $PhysicalPath 'web.config'
$offline = Join-Path $PhysicalPath 'app_offline.htm'
$keepalive = Join-Path $PSScriptRoot 'keepalive.ps1'
$page = '<html><body>Updating, back in a minute.</body></html>'

if (-not (Test-Path $SourceExe)) {
    throw "Source exe not found: $SourceExe"
}
foreach ($required in @($target, $webConfig, $keepalive)) {
    if (-not (Test-Path $required)) {
        throw "Missing $required. Run setup-iis.ps1 first."
    }
}

Set-Content -Path $offline -Value $page
try {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $locked = $true
    while ($locked) {
        try {
            $mode = [IO.FileShare]::None
            $access = [IO.FileAccess]::ReadWrite
            $open = [IO.FileMode]::Open
            $fs = [IO.File]::Open($target, $open, $access, $mode)
            $fs.Dispose()
            $locked = $false
        }
        catch [IO.IOException], [UnauthorizedAccessException] {
            if ((Get-Date) -gt $deadline) {
                throw "api.exe still locked after $TimeoutSeconds s."
            }
            Start-Sleep -Seconds 1
        }
    }
    Copy-Item -Path $target -Destination $backup -Force
    Copy-Item -Path $SourceExe -Destination $staged -Force
    Move-Item -Path $staged -Destination $target -Force
    Write-Host "Updated $target (previous kept as api.exe.bak)"
}
finally {
    Remove-Item $staged -Force -ErrorAction SilentlyContinue
    try {
        Remove-Item $offline -Force -ErrorAction Stop
    }
    catch {
        Write-Warning "Could not remove $offline"
        Write-Warning 'The site stays OFFLINE until it is deleted.'
    }
}

& $keepalive -HostName $HostName -TimeoutSeconds $TimeoutSeconds
if ($LASTEXITCODE -ne 0) {
    throw 'Warm-up failed: api.exe did not come back. See logs.'
}
Write-Host 'Site is back up.'
