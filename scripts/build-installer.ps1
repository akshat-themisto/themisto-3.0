param(
    [string]$ConfigFile = "",
    [string]$BootstrapCertDir = "",
    [string]$Output = "dist\\ThemistoSetup.exe",
    [switch]$SkipDesktopBuild
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$assetConfig = Join-Path $root "cmd\\installer\\assets\\agent.json"
$bootstrapDir = Join-Path $root "cmd\\installer\\assets\\bootstrap"
$builtDesktop = Join-Path $root "desktop\\build\\cmd\\installer\\assets\\themisto-desktop.exe"
$stagedDesktop = Join-Path $root "cmd\\installer\\assets\\themisto-desktop.exe"
$agentAsset = Join-Path $root "cmd\\installer\\assets\\themisto-agent.exe"
$outputPath = if ([System.IO.Path]::IsPathRooted($Output)) { $Output } else { Join-Path $root $Output }

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("themisto-installer-stage-" + [guid]::NewGuid().ToString("N"))
$backupConfig = Join-Path $tempDir "agent.json"
$backupBootstrap = Join-Path $tempDir "bootstrap"

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true, Position = 0)]
        [string]$FilePath,

        [Parameter(Position = 1, ValueFromRemainingArguments = $true)]
        [string[]]$Arguments
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed ($LASTEXITCODE): $FilePath $($Arguments -join ' ')"
    }
}

function Restore-StagedAssets {
    if (Test-Path $backupConfig) {
        Copy-Item $backupConfig $assetConfig -Force
    }
    if (Test-Path $backupBootstrap) {
        Get-ChildItem $bootstrapDir -File -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -ne "README.txt" } |
            Remove-Item -Force -ErrorAction SilentlyContinue
        Copy-Item (Join-Path $backupBootstrap "*") $bootstrapDir -Force -Recurse
    }
    if (Test-Path $tempDir) {
        Remove-Item $tempDir -Recurse -Force
    }
}

New-Item -ItemType Directory -Force $tempDir | Out-Null
New-Item -ItemType Directory -Force $backupBootstrap | Out-Null
Copy-Item $assetConfig $backupConfig -Force
Get-ChildItem $bootstrapDir -File -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -ne "README.txt" } |
    Copy-Item -Destination $backupBootstrap -Force

try {
    if ($ConfigFile) {
        $resolvedConfig = (Resolve-Path $ConfigFile).Path
        Get-Content $resolvedConfig -Raw | ConvertFrom-Json | Out-Null
        Copy-Item $resolvedConfig $assetConfig -Force
    }

    Get-ChildItem $bootstrapDir -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -ne "README.txt" } |
        Remove-Item -Force -ErrorAction SilentlyContinue

    if ($BootstrapCertDir) {
        $resolvedCertDir = (Resolve-Path $BootstrapCertDir).Path
        foreach ($name in @("device.crt", "device.key", "ca-chain.pem")) {
            $source = Join-Path $resolvedCertDir $name
            if (-not (Test-Path $source)) {
                throw "bootstrap credential missing: $source"
            }
            Copy-Item $source (Join-Path $bootstrapDir $name) -Force
        }
    }

    $env:GOCACHE = Join-Path $root ".gocache"

    Invoke-Native -FilePath "go" -Arguments @("build", "-buildvcs=false", "-o", $agentAsset, ".\\cmd\\agent")

    if (-not $SkipDesktopBuild) {
        Push-Location (Join-Path $root "desktop")
        try {
            Invoke-Native -FilePath "wails" -Arguments @("build", "-clean", "-platform", "windows/amd64", "-o", "..\\cmd\\installer\\assets\\themisto-desktop.exe")
        }
        finally {
            Pop-Location
        }

        Copy-Item $builtDesktop $stagedDesktop -Force
    }

    Invoke-Native -FilePath "go" -Arguments @("run", ".\\cmd\\packext")

    $outputDir = Split-Path -Parent $outputPath
    if ($outputDir) {
        New-Item -ItemType Directory -Force $outputDir | Out-Null
    }
    Invoke-Native -FilePath "go" -Arguments @("build", "-buildvcs=false", "-o", $outputPath, ".\\cmd\\installer")

    Write-Host "Built installer:" $outputPath
    if ($ConfigFile) {
        Write-Host "Embedded config:" $resolvedConfig
    }
    if ($BootstrapCertDir) {
        Write-Host "Embedded bootstrap credentials from:" $resolvedCertDir
    }
    if ($SkipDesktopBuild) {
        Write-Host "Desktop asset reused without rebuilding."
    }
}
finally {
    Restore-StagedAssets
}
