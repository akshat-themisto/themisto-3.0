param(
    [string]$GatewayURL = "https://localhost:9543",
    [string]$ListenAddr = "127.0.0.1:19090",
    [string]$PromptListenAddr = "127.0.0.1:17175",
    [string]$CertDir = "C:\ProgramData\Themisto\certs",
    [string]$GatewaySemanticTimeout = "900ms"
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$WorkDir = Join-Path $Root ".codex-tmp\agent-prompt-dev"
$AgentExe = Join-Path $WorkDir "themisto-agent.exe"
$ConfigPath = Join-Path $WorkDir "agent.json"
$CertPath = Join-Path $CertDir "device.crt"
$KeyPath = Join-Path $CertDir "device.key"
$CAPath = Join-Path $CertDir "ca-chain.pem"

function Test-Port($Port) {
    return [bool](Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $Port -ErrorAction SilentlyContinue)
}

if (-not (Test-Path -LiteralPath $CertPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $KeyPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $CAPath -PathType Leaf)) {
    throw "Missing ProgramData Themisto certs. Expected $CertPath, $KeyPath, and $CAPath."
}

if (-not (Test-Port 17177)) {
	Write-Warning "No local semantic classifier is listening on 127.0.0.1:17177. Prompt semantics will be unavailable until one starts."
}

New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null

Write-Host "Building dev Themisto agent..."
Push-Location $Root
try {
    $env:GOCACHE = Join-Path $Root ".gocache-agent-dev"
    Write-Host "Using Go build cache: $env:GOCACHE"
    go build -v -o $AgentExe .\cmd\agent
} finally {
    Pop-Location
}

$cfg = [ordered]@{
    agent_id = "prompt-dev-device"
    gateway_url = $GatewayURL
    default_decision = "forward"
    listen_addr = $ListenAddr
    prompt_capture_listen_addr = $PromptListenAddr
    telemetry_flush_interval = "5s"
    policy_sync_interval = "10s"
    cert_path = $CertPath
    key_path = $KeyPath
    ca_path = $CAPath
    prompt_semantics_enabled = $true
    prompt_semantics_local_url = "http://127.0.0.1:17177/v1/classify"
    prompt_semantics_gateway_enabled = $true
    prompt_semantics_local_timeout = "1500ms"
    prompt_semantics_gateway_timeout = $GatewaySemanticTimeout
    prompt_semantics_block_threshold = 0.68
    prompt_semantics_alert_threshold = 0.55
    prompt_semantics_ambiguous_threshold = 0.55
}

$cfg | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ConfigPath -Encoding ascii

Write-Host ""
Write-Host "Starting dev agent prompt service."
Write-Host "Prompt API: http://$PromptListenAddr/v1/prompt/evaluate"
Write-Host "Proxy listen addr for this dev run: $ListenAddr"
Write-Host "Press Ctrl+C to stop. The agent may temporarily register the system proxy while running."
Write-Host ""

& $AgentExe -config $ConfigPath
