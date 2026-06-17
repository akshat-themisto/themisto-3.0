param(
    [string]$Version = "0.1.0",
    [string]$OutDir = "dist/installers",
    [string]$ConfigPath = ""
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$BundleDir = Join-Path $env:TEMP ("themisto-win-bundle-" + [Guid]::NewGuid().ToString("N"))
$OutDirAbs = Join-Path $RepoRoot $OutDir
$ZipPath = Join-Path $OutDirAbs ("ThemistoAgent-windows-" + $Version + ".zip")

New-Item -ItemType Directory -Force -Path $BundleDir | Out-Null
New-Item -ItemType Directory -Force -Path $OutDirAbs | Out-Null

Push-Location $RepoRoot
try {
    Write-Host "==> Building Windows agent binary..."
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    $env:GOCACHE = (Join-Path $RepoRoot ".gocache")
    $env:GOMODCACHE = (Join-Path $RepoRoot ".gomodcache")
    go build -buildvcs=false -o (Join-Path $BundleDir "themisto-agent.exe") ./cmd/agent
}
finally {
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOCACHE -ErrorAction SilentlyContinue
    Remove-Item Env:GOMODCACHE -ErrorAction SilentlyContinue
    Pop-Location
}

Copy-Item -Force (Join-Path $RepoRoot "installer/windows/install.ps1") (Join-Path $BundleDir "install.ps1")
Copy-Item -Force (Join-Path $RepoRoot "installer/windows/install.bat") (Join-Path $BundleDir "install.bat")
Copy-Item -Force (Join-Path $RepoRoot "installer/windows/uninstall.ps1") (Join-Path $BundleDir "uninstall.ps1")
Copy-Item -Force (Join-Path $RepoRoot "installer/windows/uninstall.bat") (Join-Path $BundleDir "uninstall.bat")
Copy-Item -Force (Join-Path $RepoRoot "installer/windows/themisto-service.ps1") (Join-Path $BundleDir "themisto-service.ps1")
Copy-Item -Force (Join-Path $RepoRoot "installer/windows/themisto-activate.ps1") (Join-Path $BundleDir "themisto-activate.ps1")
Copy-Item -Recurse -Force (Join-Path $RepoRoot "semantic-classifier/deberta") (Join-Path $BundleDir "classifier")
if ($ConfigPath -ne "") {
    Copy-Item -Force $ConfigPath (Join-Path $BundleDir "agent.json")
} else {
    Copy-Item -Force (Join-Path $RepoRoot "installer/windows/agent.json") (Join-Path $BundleDir "agent.json")
}

if (Test-Path $ZipPath) {
    Remove-Item -Force $ZipPath
}
Compress-Archive -Path (Join-Path $BundleDir "*") -DestinationPath $ZipPath

Remove-Item -Recurse -Force $BundleDir
Write-Host "==> Done: $ZipPath"
