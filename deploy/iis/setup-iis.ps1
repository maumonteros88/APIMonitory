<#
.SYNOPSIS
  One-time (idempotent) IIS setup for APIMonitory. Run as admin.
.DESCRIPTION
  Creates/updates its own app pool and site only. It never runs
  iisreset and never touches server-level config, other sites or
  other app pools. DATABASE_URL and API_TOKEN are asked for
  interactively and stored as app pool environment variables.
  The HTTPS binding is left to win-acme (wacs.exe).
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$HostName,
    [string]$SiteName = 'APIMonitory',
    [string]$PhysicalPath = 'C:\inetpub\APIMonitory\web',
    [string]$AppTimezone
)

$ErrorActionPreference = 'Stop'

# --- Fail fast when not elevated -------------------------------
$winId = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($winId)
$adminRole = [Security.Principal.WindowsBuiltInRole]::Administrator
if (-not $principal.IsInRole($adminRole)) {
    throw 'Run this script from an elevated PowerShell.'
}

Import-Module WebAdministration

$marshal = [Runtime.InteropServices.Marshal]
$apphost = 'MACHINE/WEBROOT/APPHOST'
$poolsPath = '/system.applicationHost/applicationPools'
$sitesPath = '/system.applicationHost/sites'
$poolPath = "IIS:\AppPools\$SiteName"
$sitePath = "IIS:\Sites\$SiteName"

function Read-Secret {
    param([string]$Prompt)
    $secure = Read-Host -Prompt $Prompt -AsSecureString
    $ptr = $marshal::SecureStringToBSTR($secure)
    try {
        $plain = $marshal::PtrToStringBSTR($ptr)
    }
    finally {
        $marshal::ZeroFreeBSTR($ptr)
        $secure.Dispose()
    }
    if ([string]::IsNullOrEmpty($plain)) {
        throw "$Prompt cannot be empty."
    }
    # Ctrl+V in a SecureString prompt types char 0x16 instead of
    # pasting; IIS then rejects it as an invalid XML character and
    # leaves the PowerShell session's IIS config cache dirty.
    if ($plain -match '[\x00-\x1F\x7F]') {
        $msg = "$Prompt contains a control character."
        throw "$msg Paste with right-click, not Ctrl+V."
    }
    return $plain
}

function Set-PoolEnv {
    param([string]$Name, [string]$Value)
    $coll = "$poolsPath/add[@name='$SiteName']/environmentVariables"
    $entry = "$coll/add[@name='$Name']"
    $find = @{ PSPath = $apphost; Filter = $entry }
    if (Get-WebConfiguration @find) {
        $set = @{
            PSPath = $apphost
            Filter = $entry
            Name   = 'value'
            Value  = $Value
        }
        Set-WebConfigurationProperty @set
    }
    else {
        $add = @{
            PSPath = $apphost
            Filter = $coll
            Name   = '.'
            Value  = @{ name = $Name; value = $Value }
        }
        Add-WebConfigurationProperty @add
    }
}

# --- Ownership guard: never touch someone else's pool or site ---
# A typo in -SiteName (e.g. 'Default Web Site') must not modify an
# existing site or pool that is not ours.
function Resolve-Dir {
    param([string]$Path)
    $expanded = [Environment]::ExpandEnvironmentVariables($Path)
    return $expanded.TrimEnd('\')
}
$wantPath = Resolve-Dir $PhysicalPath
$binding = "*:80:$HostName"

$ourSite = Get-Website | Where-Object { $_.name -ieq $SiteName }
if ($ourSite) {
    $havePath = Resolve-Dir $ourSite.physicalPath
    if ($havePath -ine $wantPath) {
        throw "Site '$SiteName' exists with another path: $havePath"
    }
}
foreach ($other in Get-Website) {
    if ($other.name -ieq $SiteName) { continue }
    $usedPools = @($other.applicationPool)
    $apps = Get-WebApplication -Site $other.name
    foreach ($app in $apps) { $usedPools += $app.applicationPool }
    if ($usedPools -contains $SiteName) {
        throw "App pool '$SiteName' is used by site '$($other.name)'."
    }
    foreach ($b in $other.bindings.Collection) {
        if ($b.bindingInformation -ieq $binding) {
            throw "Binding $binding is taken by '$($other.name)'."
        }
    }
}

# --- Secrets: prompted, never echoed, never logged --------------
$dbUrl = Read-Secret 'DATABASE_URL'
if ($dbUrl -notmatch '^postgres(ql)?://') {
    throw 'DATABASE_URL must start with postgres://'
}
$apiToken = Read-Secret 'API_TOKEN'

# --- Folders ----------------------------------------------------
$logsPath = Join-Path $PhysicalPath 'logs'
New-Item -ItemType Directory -Force -Path $logsPath | Out-Null

# --- App pool ---------------------------------------------------
if (-not (Test-Path $poolPath)) {
    New-WebAppPool -Name $SiteName | Out-Null
    Write-Host "Created app pool $SiteName"
}
# disallowOverlappingRotation: on an overlapped recycle the old
# api.exe never drains (open SSE streams), so two schedulers
# would run at once.
$zero = [TimeSpan]::Zero
$poolProps = [ordered]@{
    'managedRuntimeVersion'                 = ''
    'autoStart'                             = $true
    'startMode'                             = 'AlwaysRunning'
    'processModel.idleTimeout'              = $zero
    'processModel.maxProcesses'             = 1
    'recycling.periodicRestart.time'        = $zero
    'recycling.disallowOverlappingRotation' = $true
}
foreach ($key in $poolProps.Keys) {
    $value = $poolProps[$key]
    Set-ItemProperty -Path $poolPath -Name $key -Value $value
}

# --- Pool environment variables (stored in applicationHost) -----
Set-PoolEnv -Name 'DATABASE_URL' -Value $dbUrl
Set-PoolEnv -Name 'API_TOKEN' -Value $apiToken
$envNames = @('DATABASE_URL', 'API_TOKEN')
if ($AppTimezone) {
    Set-PoolEnv -Name 'APP_TIMEZONE' -Value $AppTimezone
    $envNames += 'APP_TIMEZONE'
}
$dbUrl = $null
$apiToken = $null

# --- ACLs: read+execute on the site, modify on logs (stdout) ----
$poolItem = Get-ItemProperty -Path $poolPath
$idType = [string]$poolItem.processModel.identityType
if ($idType -eq 'ApplicationPoolIdentity') {
    $account = "IIS AppPool\$SiteName"
}
else {
    $account = $poolItem.processModel.userName
}
function Grant-Acl {
    param([string]$Path, [string]$Perm)
    $grant = "${account}:(OI)(CI)$Perm"
    icacls $Path /grant $grant | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "icacls failed granting $Perm to $account on $Path."
    }
}
Grant-Acl -Path $PhysicalPath -Perm 'RX'
Grant-Acl -Path $logsPath -Perm 'M'

# --- Site -------------------------------------------------------
if (-not (Test-Path $sitePath)) {
    $newSite = @{
        Name            = $SiteName
        PhysicalPath    = $PhysicalPath
        ApplicationPool = $SiteName
        IPAddress       = '*'
        Port            = 80
        HostHeader      = $HostName
    }
    New-Website @newSite | Out-Null
    Write-Host "Created site $SiteName"
}
else {
    Set-ItemProperty $sitePath -Name physicalPath -Value $PhysicalPath
    Set-ItemProperty $sitePath -Name applicationPool -Value $SiteName
    $found = Get-WebBinding -Name $SiteName -Protocol http |
        Where-Object { $_.bindingInformation -ieq $binding }
    if (-not $found) {
        $newBinding = @{
            Name       = $SiteName
            Protocol   = 'http'
            IPAddress  = '*'
            Port       = 80
            HostHeader = $HostName
        }
        New-WebBinding @newBinding
    }
}

# --- Preload the root application (starts api.exe on pool start)
$rootApp = "$sitesPath/site[@name='$SiteName']/application[@path='/']"
$preload = @{
    PSPath = $apphost
    Filter = $rootApp
    Name   = 'preloadEnabled'
    Value  = $true
}
Set-WebConfigurationProperty @preload

# --- Start only our own pool and site ---------------------------
$poolState = (Get-WebAppPoolState -Name $SiteName).Value
if ($poolState -eq 'Stopped') {
    Start-WebAppPool -Name $SiteName
}
else {
    # Pool config changed, so IIS recycles it by itself; this
    # makes sure the new environment variables are picked up.
    Restart-WebAppPool -Name $SiteName
}
$siteState = (Get-WebsiteState -Name $SiteName).Value
if ($siteState -eq 'Stopped') {
    Start-Website -Name $SiteName
}

# --- Keep-alive task: ANCM relaunches api.exe only on a request -
# The script is copied next to (not inside) the site folder, so it
# is never served. Only this one task is created or replaced.
$taskName = "$SiteName keep-alive"
$kaDir = Split-Path $wantPath -Parent
$kaScript = Join-Path $kaDir 'keepalive.ps1'
$kaSource = Join-Path $PSScriptRoot 'keepalive.ps1'
if (-not (Test-Path $kaSource)) {
    throw 'keepalive.ps1 not found next to this script.'
}
Copy-Item -Path $kaSource -Destination $kaScript -Force
$kaFile = '-File "{0}"' -f $kaScript
$kaOpts = '-NoProfile -NonInteractive -ExecutionPolicy Bypass'
$kaArgs = "$kaOpts $kaFile -HostName $HostName -TimeoutSeconds 30"
$kaAction = @{
    Execute  = 'powershell.exe'
    Argument = $kaArgs
}
$kaTrigger = @{
    Once               = $true
    At                 = (Get-Date)
    RepetitionInterval = (New-TimeSpan -Minutes 2)
}
$kaPrincipal = @{
    UserId    = 'SYSTEM'
    LogonType = 'ServiceAccount'
    RunLevel  = 'Highest'
}
$kaSettings = @{
    ExecutionTimeLimit = (New-TimeSpan -Minutes 5)
    MultipleInstances  = 'IgnoreNew'
    StartWhenAvailable = $true
}
$register = @{
    TaskName  = $taskName
    Action    = New-ScheduledTaskAction @kaAction
    Trigger   = New-ScheduledTaskTrigger @kaTrigger
    Principal = New-ScheduledTaskPrincipal @kaPrincipal
    Settings  = New-ScheduledTaskSettingsSet @kaSettings
    Force     = $true
}
Register-ScheduledTask @register | Out-Null

# --- Summary (no secrets) ---------------------------------------
$exePath = Join-Path $PhysicalPath 'api.exe'
$cfgPath = Join-Path $PhysicalPath 'web.config'
Write-Host ''
Write-Host '--- APIMonitory IIS setup ---'
Write-Host "Site          : $SiteName"
Write-Host "Host binding  : http $binding"
Write-Host "Physical path : $PhysicalPath"
Write-Host "App pool      : $SiteName (No Managed Code)"
Write-Host "Pool env vars : $($envNames -join ', ')"
Write-Host "Keep-alive    : '$taskName' every 2 min"
Write-Host "Logs          : $logsPath (modify for $account)"
Write-Host "Pool state    : $((Get-WebAppPoolState $SiteName).Value)"
Write-Host "Site state    : $((Get-WebsiteState $SiteName).Value)"
if (-not (Test-Path $exePath)) {
    Write-Warning "api.exe not found in $PhysicalPath yet."
}
if (-not (Test-Path $cfgPath)) {
    Write-Warning "web.config not found in $PhysicalPath yet."
}
Write-Host 'Next: DNS A record, then wacs.exe for HTTPS.'
