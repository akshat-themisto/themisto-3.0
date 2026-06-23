param(
    [string]$ModelId = "MoritzLaurer/deberta-v3-xsmall-zeroshot-v1.1-all-33",
    [string]$Destination = "C:\ProgramData\Themisto\models\deberta",
    [string]$PythonExe = "python"
)

$ErrorActionPreference = "Stop"

$Destination = [System.IO.Path]::GetFullPath($Destination)
$Staging = "$Destination.download"
$Backup = "$Destination.backup"

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
Remove-Item -LiteralPath $Staging -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $Staging | Out-Null

$script = @"
from huggingface_hub import snapshot_download
snapshot_download(
    repo_id=r"$ModelId",
    local_dir=r"$Staging",
    allow_patterns=[
        "config.json", "pytorch_model.bin", "model.safetensors",
        "tokenizer.json", "tokenizer_config.json", "special_tokens_map.json",
        "spm.model", "added_tokens.json",
    ],
)
"@

$tmp = New-TemporaryFile
try {
    Set-Content -LiteralPath $tmp -Value $script -Encoding UTF8
    & $PythonExe -m pip install "huggingface_hub>=0.24,<1.0"
    if ($LASTEXITCODE -ne 0) { throw "Failed to install huggingface_hub." }
    & $PythonExe $tmp
    if ($LASTEXITCODE -ne 0) { throw "Failed to download $ModelId." }

    if (-not (Test-Path -LiteralPath (Join-Path $Staging "config.json") -PathType Leaf)) {
        throw "Downloaded model is missing config.json."
    }
    if (-not (Test-Path -LiteralPath (Join-Path $Staging "pytorch_model.bin") -PathType Leaf) -and
        -not (Test-Path -LiteralPath (Join-Path $Staging "model.safetensors") -PathType Leaf)) {
        throw "Downloaded model is missing model weights."
    }

    Remove-Item -LiteralPath $Backup -Recurse -Force -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $Destination) {
        Move-Item -LiteralPath $Destination -Destination $Backup
    }
    Move-Item -LiteralPath $Staging -Destination $Destination
    Remove-Item -LiteralPath $Backup -Recurse -Force -ErrorAction SilentlyContinue
} finally {
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
}

Write-Host "Model downloaded to $Destination"
