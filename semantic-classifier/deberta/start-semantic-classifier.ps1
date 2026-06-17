param(
    [string]$ModelDir = "C:\ProgramData\Themisto\models\deberta",
    [string]$ClassifierDir = "C:\Program Files\Themisto\classifier",
    [int]$Port = 17177,
    [string]$HostAddress = "127.0.0.1"
)

$ErrorActionPreference = "Stop"

$Server = Join-Path $ClassifierDir "server.py"
$Requirements = Join-Path $ClassifierDir "requirements.txt"
$Venv = Join-Path $ClassifierDir ".venv"
$Python = Join-Path $Venv "Scripts\python.exe"
$LogDir = "C:\ProgramData\Themisto\logs"
$LogPath = Join-Path $LogDir "semantic-classifier.log"

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

function Write-ClassifierLog([string]$Message) {
    $line = "{0} {1}" -f (Get-Date).ToString("o"), $Message
    Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
}

if (-not (Test-Path -LiteralPath $Server -PathType Leaf)) {
    throw "Classifier server not found: $Server"
}
if (-not (Test-Path -LiteralPath $ModelDir -PathType Container)) {
    throw "DeBERTa model directory not found: $ModelDir"
}

if (-not (Test-Path -LiteralPath $Python -PathType Leaf)) {
    $systemPython = (Get-Command py.exe -ErrorAction SilentlyContinue)
    if ($systemPython) {
        & py -3 -m venv $Venv
    } else {
        $systemPython = (Get-Command python.exe -ErrorAction SilentlyContinue)
        if (-not $systemPython) {
            throw "Python 3 is required to create the local semantic classifier runtime."
        }
        & python -m venv $Venv
    }
    & $Python -m pip install --upgrade pip
    if (Test-Path -LiteralPath $Requirements -PathType Leaf) {
        & $Python -m pip install -r $Requirements
    }
}

$env:THEMISTO_SEMANTIC_MODEL_DIR = $ModelDir
$env:THEMISTO_SEMANTIC_LABELS = Join-Path $ClassifierDir "labels.json"
$env:PYTHONUNBUFFERED = "1"

Write-ClassifierLog "starting semantic classifier on http://$HostAddress`:$Port with model $ModelDir"
& $Python -m uvicorn server:app --host $HostAddress --port $Port --app-dir $ClassifierDir 2>&1 |
    ForEach-Object { Write-ClassifierLog ([string]$_) }
