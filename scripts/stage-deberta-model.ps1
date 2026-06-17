param(
    [string]$ModelDir = "C:\ProgramData\Themisto\models\deberta",
    [string]$InstallerAssetDir = ""
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
if ([string]::IsNullOrWhiteSpace($InstallerAssetDir)) {
    $InstallerAssetDir = Join-Path $Root "cmd\installer\assets\classifier\model"
}

if (-not (Test-Path -LiteralPath $ModelDir -PathType Container)) {
    throw "Model directory not found: $ModelDir"
}

$required = @("config.json")
foreach ($name in $required) {
    $path = Join-Path $ModelDir $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Model directory is missing $name"
    }
}

if (-not (Test-Path -LiteralPath (Join-Path $ModelDir "tokenizer.json") -PathType Leaf) -and
    -not (Test-Path -LiteralPath (Join-Path $ModelDir "spm.model") -PathType Leaf)) {
    throw "Model directory must include tokenizer.json or spm.model"
}

if (-not (Test-Path -LiteralPath (Join-Path $ModelDir "model.safetensors") -PathType Leaf) -and
    -not (Test-Path -LiteralPath (Join-Path $ModelDir "pytorch_model.bin") -PathType Leaf)) {
    throw "Model directory must include model.safetensors or pytorch_model.bin"
}

if (Test-Path -LiteralPath $InstallerAssetDir) {
    Remove-Item -Recurse -Force -LiteralPath $InstallerAssetDir
}
New-Item -ItemType Directory -Force -Path $InstallerAssetDir | Out-Null
Get-ChildItem -LiteralPath $ModelDir -Force | Copy-Item -Recurse -Force -Destination $InstallerAssetDir

Write-Host "Staged DeBERTa model into $InstallerAssetDir"
