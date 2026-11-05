<#
.SYNOPSIS
  Warms up APIMonitory so ANCM (re)launches api.exe.
.DESCRIPTION
  GETs http://127.0.0.1/healthz with the site's Host header and
  retries until it answers 200 or the timeout expires. ANCM only
  relaunches a stopped or crashed api.exe on the next request, so
  without this the scheduler stays dead until someone visits.
  Exit code 0 on success, 1 on failure.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$HostName,
    [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = 'Stop'

$url = 'http://127.0.0.1/healthz'
$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
$lastError = 'no attempt made'

do {
    $response = $null
    try {
        $req = [Net.HttpWebRequest]::Create($url)
        $req.Host = $HostName
        $req.Timeout = 10000
        $req.Proxy = $null
        $req.AllowAutoRedirect = $false
        $response = $req.GetResponse()
        if ([int]$response.StatusCode -eq 200) {
            Write-Host "healthz OK for $HostName"
            exit 0
        }
        $lastError = "HTTP $([int]$response.StatusCode)"
    }
    catch [Net.WebException] {
        $lastError = $_.Exception.Message
        $failed = $_.Exception.Response
        if ($failed) {
            $lastError = "HTTP $([int]$failed.StatusCode)"
        }
    }
    finally {
        if ($response) { $response.Close() }
    }
    Start-Sleep -Seconds 3
} while ((Get-Date) -lt $deadline)

Write-Warning "healthz for $HostName failed: $lastError"
Write-Warning "No 200 in $TimeoutSeconds s. Check logs\stdout_*.log."
exit 1
