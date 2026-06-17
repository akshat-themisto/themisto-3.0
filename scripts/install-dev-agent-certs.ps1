param(
    [string]$SourceCertDir = "C:\Themisto labs\main 2.0\certs\devices\themisto-dev-windows",
    [string]$InstallCertDir = "C:\ProgramData\Themisto\certs",
    [switch]$StopAgent
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Run this script from an elevated PowerShell window."
    }
}

function Assert-CertFile {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing required cert file: $Path"
    }
}

Assert-Admin

$deviceCrt = Join-Path $SourceCertDir "device.crt"
$deviceKey = Join-Path $SourceCertDir "device.key"
$caChain = Join-Path $SourceCertDir "ca-chain.pem"
Assert-CertFile $deviceCrt
Assert-CertFile $deviceKey
Assert-CertFile $caChain

if ($StopAgent) {
    Get-Process -Name themisto-agent -ErrorAction SilentlyContinue | Stop-Process -Force
}

if (Test-Path -LiteralPath $InstallCertDir) {
    $backup = "$InstallCertDir.backup-$(Get-Date -Format 'yyyyMMdd-HHmmss')"
    Copy-Item -LiteralPath $InstallCertDir -Destination $backup -Recurse -Force
    Write-Host "Backed up existing certs to $backup"
}

New-Item -ItemType Directory -Force -Path $InstallCertDir | Out-Null
Copy-Item -LiteralPath $deviceCrt -Destination (Join-Path $InstallCertDir "device.crt") -Force
Copy-Item -LiteralPath $deviceKey -Destination (Join-Path $InstallCertDir "device.key") -Force
Copy-Item -LiteralPath $caChain -Destination (Join-Path $InstallCertDir "ca-chain.pem") -Force

Write-Host "Installed Themisto dev agent certs to $InstallCertDir"
