param(
    [string]$Model = "llama3.2:latest",
    [string]$Source = "gateway_llama",
    [string]$HostAddress = "127.0.0.1",
    [int]$Port = 17178,
    [string]$OllamaURL = "http://127.0.0.1:11434/api/chat",
    [string]$PythonExe = ""
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Script = Join-Path $Root "scripts\start-qwen-ollama-classifier.py"

if ([string]::IsNullOrWhiteSpace($PythonExe)) {
    $cmd = Get-Command python.exe -ErrorAction SilentlyContinue
    if ($cmd) {
        $PythonExe = $cmd.Source
    } else {
        $codexPython = Join-Path $env:USERPROFILE ".cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe"
        if (Test-Path -LiteralPath $codexPython -PathType Leaf) {
            $PythonExe = $codexPython
        } else {
            throw "Python was not found. Pass -PythonExe with a Python executable path."
        }
    }
}

$env:NO_PROXY = "127.0.0.1,localhost"
$env:no_proxy = "127.0.0.1,localhost"
$env:HTTP_PROXY = ""
$env:HTTPS_PROXY = ""
$env:http_proxy = ""
$env:https_proxy = ""

& $PythonExe $Script --host $HostAddress --port $Port --ollama-url $OllamaURL --model $Model --source $Source
