# ARX MDM agent — zero-touch install for Windows (native service).
# Required env: ARX_SERVER_URL (e.g. https://mdm.example.com), ARX_ENROLL_TOKEN (presentation secret).
# Optional: ARX_INSECURE_TLS=1 to allow self-signed TLS when downloading (lab only).
#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$server = [string]$env:ARX_SERVER_URL
$token = [string]$env:ARX_ENROLL_TOKEN
if ([string]::IsNullOrWhiteSpace($server) -or [string]::IsNullOrWhiteSpace($token)) {
  Write-Error 'Set ARX_SERVER_URL and ARX_ENROLL_TOKEN, then re-run in an elevated PowerShell.'
}

if ($env:ARX_INSECURE_TLS -eq '1') {
  [System.Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
}

$base = $server.TrimEnd('/')
$destDir = 'C:\Program Files\ARX'
$exePath = Join-Path $destDir 'arx-agent.exe'
$certDir = Join-Path $destDir 'certs'

New-Item -ItemType Directory -Force -Path $destDir | Out-Null
New-Item -ItemType Directory -Force -Path $certDir | Out-Null

$uri = "$base/v1/install/bin/windows"
Invoke-WebRequest -Uri $uri -OutFile $exePath -UseBasicParsing

$p = Start-Process -FilePath $exePath -ArgumentList @(
  'enroll', '-server', $server, '-token', $token, '-certdir', $certDir
) -Wait -PassThru -NoNewWindow
if ($p.ExitCode -ne 0) { exit $p.ExitCode }

$p2 = Start-Process -FilePath $exePath -ArgumentList @(
  '-install', '-server', $server, '-certdir', $certDir
) -Wait -PassThru -NoNewWindow
if ($p2.ExitCode -ne 0) { exit $p2.ExitCode }
