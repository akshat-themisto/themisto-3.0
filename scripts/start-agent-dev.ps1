param(
    [switch]$RealDeberta,
    [switch]$MockQwen,
    [switch]$GatewayLLM,
    [string]$GatewayLLMModel = "llama3.2:latest",
    [int]$GatewayLLMPort = 17179,
    [string]$GatewayURL = "https://localhost:9543",
    [string]$ListenAddr = "127.0.0.1:19090",
    [string]$PromptListenAddr = "127.0.0.1:17175",
    [string]$CertDir = "C:\ProgramData\Themisto\certs",
    [string]$GatewaySemanticTimeout = ""
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$HelperJobs = @()

function Test-Port($Port) {
    return [bool](Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $Port -ErrorAction SilentlyContinue)
}

function Start-HelperJob {
    param(
        [string]$Name,
        [string]$Command,
        [int]$Port
    )

    if (Test-Port $Port) {
        Write-Host "$Name already listening on 127.0.0.1:$Port"
        return
    }

    $job = Start-Job -Name $Name -ScriptBlock {
        param([string]$CommandText)
        Invoke-Expression $CommandText
    } -ArgumentList $Command

    $script:HelperJobs += $job
    Start-Sleep -Milliseconds 800

    if ($job.State -eq "Failed") {
        Receive-Job -Job $job
        throw "$Name failed to start."
    }

    Write-Host "Started $Name on 127.0.0.1:$Port"
}

Set-Location $Root

try {
    if ($RealDeberta) {
        $classifier = Join-Path $Root "semantic-classifier\deberta\start-semantic-classifier.ps1"
        Start-HelperJob `
            -Name "ThemistoRealDebertaDev" `
            -Port 17177 `
            -Command "powershell -NoProfile -ExecutionPolicy Bypass -File `"$classifier`" -ClassifierDir `"$Root\semantic-classifier\deberta`" -ModelDir `"C:\ProgramData\Themisto\models\deberta`""
    } else {
        $mockDeberta = Join-Path $Root "scripts\mock-deberta-classifier.ps1"
        Start-HelperJob `
            -Name "ThemistoMockDebertaDev" `
            -Port 17177 `
            -Command "powershell -NoProfile -ExecutionPolicy Bypass -File `"$mockDeberta`""
    }

    if ($GatewayLLM) {
        $ollamaClassifier = Join-Path $Root "scripts\start-ollama-classifier.ps1"
        Start-HelperJob `
            -Name "ThemistoGatewayLLMDev" `
            -Port $GatewayLLMPort `
            -Command "powershell -NoProfile -ExecutionPolicy Bypass -File `"$ollamaClassifier`" -Model `"$GatewayLLMModel`" -Port $GatewayLLMPort"
    } elseif ($MockQwen) {
        $mockQwenScript = Join-Path $Root "scripts\mock-qwen-classifier.ps1"
        Start-HelperJob `
            -Name "ThemistoMockQwenDev" `
            -Port 17178 `
            -Command "powershell -NoProfile -ExecutionPolicy Bypass -File `"$mockQwenScript`""
    }

    Write-Host ""
    Write-Host "Starting Themisto Labs dev agent..."
    Write-Host "Stop with Ctrl+C. Helper jobs started by this script will be stopped."
    Write-Host ""

    if ([string]::IsNullOrWhiteSpace($GatewaySemanticTimeout)) {
        if ($GatewayLLM) {
            $GatewaySemanticTimeout = "15s"
        } else {
            $GatewaySemanticTimeout = "900ms"
        }
    }

    & (Join-Path $Root "scripts\start-agent-prompt-dev.ps1") -GatewayURL $GatewayURL -ListenAddr $ListenAddr -PromptListenAddr $PromptListenAddr -CertDir $CertDir -GatewaySemanticTimeout $GatewaySemanticTimeout
} finally {
    foreach ($job in $HelperJobs) {
        Stop-Job -Job $job -ErrorAction SilentlyContinue
        Remove-Job -Job $job -Force -ErrorAction SilentlyContinue
    }
}
