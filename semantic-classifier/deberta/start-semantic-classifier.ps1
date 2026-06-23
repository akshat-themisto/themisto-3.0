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

try {
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
    Add-Content -LiteralPath $LogPath -Value ("{0} classifier launcher initialized" -f (Get-Date).ToString("o")) -Encoding UTF8
} catch {
    $LogDir = Join-Path $env:TEMP "Themisto"
    $LogPath = Join-Path $LogDir "semantic-classifier.log"
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
    Add-Content -LiteralPath $LogPath -Value ("{0} classifier launcher initialized; ProgramData log was not writable" -f (Get-Date).ToString("o")) -Encoding UTF8
}

function Write-ClassifierLog([string]$Message) {
    $line = "{0} {1}" -f (Get-Date).ToString("o"), $Message
    Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
}

function Invoke-LoggedPython {
    param(
        [string[]]$Arguments,
        [string]$FailureMessage
    )

    & $Python @Arguments 2>&1 | ForEach-Object {
        $message = [string]$_
        Write-ClassifierLog $message
        Write-Host $message
    }
    if ($LASTEXITCODE -ne 0) {
        throw "$FailureMessage See $LogPath for details."
    }
}

function Test-ClassifierRuntime([string]$PythonPath) {
    if (-not (Test-Path -LiteralPath $PythonPath -PathType Leaf)) {
        return $false
    }
    try {
        & $PythonPath -c "import fastapi, uvicorn, torch, sentencepiece; from transformers import AutoModel, AutoTokenizer" 2>$null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Find-SupportedPython {
    $launcher = Get-Command py.exe -ErrorAction SilentlyContinue
    if ($launcher) {
        foreach ($version in @("3.12", "3.11", "3.10")) {
            & py "-$version" -c "import sys; print(sys.executable)" *> $null
            if ($LASTEXITCODE -eq 0) {
                return @{ Command = "py"; Arguments = @("-$version") }
            }
        }
    }

    $python = Get-Command python.exe -ErrorAction SilentlyContinue
    if ($python) {
        $version = & python -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
        if ($version -in @("3.10", "3.11", "3.12")) {
            return @{ Command = $python.Source; Arguments = @() }
        }
    }
    return $null
}

if (-not (Test-Path -LiteralPath $Server -PathType Leaf)) {
    throw "Classifier server not found: $Server"
}
if (-not (Test-Path -LiteralPath $ModelDir -PathType Container)) {
    throw "DeBERTa model directory not found: $ModelDir"
}

if (-not (Test-ClassifierRuntime $Python)) {
    if (Test-Path -LiteralPath $Venv) {
        Remove-Item -LiteralPath $Venv -Recurse -Force
    }
    $systemPython = Find-SupportedPython
    if (-not $systemPython) {
        throw "Python 3.10, 3.11, or 3.12 is required for the pinned DeBERTa runtime. Install Python 3.12 with: winget install -e --id Python.Python.3.12"
    }
    & $systemPython.Command @($systemPython.Arguments) -m venv $Venv
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to create the semantic classifier virtual environment."
    }
    & $Python -m pip --version *> $null
    if ($LASTEXITCODE -ne 0) {
        & $Python -m ensurepip --upgrade 2>&1 | ForEach-Object {
            $message = [string]$_
            Write-ClassifierLog $message
            Write-Host $message
        }
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to bootstrap pip for the semantic classifier. See $LogPath for details."
        }
    }
    Invoke-LoggedPython -Arguments @("-m", "pip", "install", "--upgrade", "pip") `
        -FailureMessage "Failed to upgrade pip for the semantic classifier."
    if (Test-Path -LiteralPath $Requirements -PathType Leaf) {
        Invoke-LoggedPython -Arguments @("-m", "pip", "install", "-r", $Requirements) `
            -FailureMessage "Failed to install the semantic classifier dependencies."
    }
    if (-not (Test-ClassifierRuntime $Python)) {
        throw "The semantic classifier runtime was created, but required imports still fail."
    }
}

$env:THEMISTO_SEMANTIC_MODEL_DIR = $ModelDir
$env:THEMISTO_SEMANTIC_LABELS = Join-Path $ClassifierDir "labels.json"
$env:PYTHONUNBUFFERED = "1"

Write-ClassifierLog "starting semantic classifier on http://$HostAddress`:$Port with model $ModelDir"
$previousErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"
try {
    & $Python -m uvicorn server:app --host $HostAddress --port $Port --app-dir $ClassifierDir 2>&1 |
        ForEach-Object { Write-ClassifierLog ([string]$_) }
    $uvicornExitCode = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $previousErrorActionPreference
}
if ($uvicornExitCode -ne 0) {
    throw "Semantic classifier exited with code $uvicornExitCode. See $LogPath for details."
}
