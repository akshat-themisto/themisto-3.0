param(
    [string]$ModelId = "microsoft/deberta-v3-small",
    [string]$Destination = "C:\ProgramData\Themisto\models\deberta",
    [string]$PythonExe = "python"
)

$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path $Destination | Out-Null

$script = @"
from huggingface_hub import snapshot_download
snapshot_download(
    repo_id=r"$ModelId",
    local_dir=r"$Destination",
    local_dir_use_symlinks=False,
)
"@

$tmp = New-TemporaryFile
try {
    Set-Content -LiteralPath $tmp -Value $script -Encoding UTF8
    & $PythonExe -m pip install --upgrade huggingface_hub
    & $PythonExe $tmp
} finally {
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
}

Write-Host "Model downloaded to $Destination"
