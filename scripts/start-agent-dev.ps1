param(
    [switch]$RealDeberta,
	[switch]$MockDeberta,
	[switch]$AllowNonAdmin,
    [switch]$MockQwen,
    [switch]$GatewayLLM,
    [string]$GatewayLLMModel = "llama3.2:latest",
    [int]$GatewayLLMPort = 17179,
    [string]$GatewayURL = "https://localhost:9543",
    [string]$ListenAddr = "127.0.0.1:19090",
    [string]$PromptListenAddr = "127.0.0.1:17175",
    [string]$CertDir = "C:\ProgramData\Themisto\certs",
    [string]$DebertaModelDir = "C:\ProgramData\Themisto\models\deberta",
    [string]$GatewaySemanticTimeout = ""
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$HelperJobs = @()

function Test-IsAdministrator {
	$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
	$principal = [Security.Principal.WindowsPrincipal]::new($identity)
	return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if ($RealDeberta -and $MockDeberta) {
	throw "Choose either -RealDeberta or -MockDeberta, not both."
}

if (-not $AllowNonAdmin -and -not (Test-IsAdministrator)) {
	throw "Run this script from Administrator PowerShell. Use -AllowNonAdmin only for capture-only development without Windows proxy registration."
}

function Test-Port($Port) {
    return [bool](Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $Port -ErrorAction SilentlyContinue)
}

function Test-HelperReady([int]$Port, [string]$HealthURL) {
    if (-not (Test-Port $Port)) {
        return $false
    }
    if ([string]::IsNullOrWhiteSpace($HealthURL)) {
        return $true
    }
    try {
        $health = Invoke-RestMethod -Uri $HealthURL -TimeoutSec 3
        return [bool]$health.ok -and $health.model_mode -eq "zero_shot_nli"
    } catch {
        return $false
    }
}

function Start-HelperJob {
    param(
        [string]$Name,
        [string]$Command,
        [int]$Port,
        [string]$HealthURL = ""
    )

    if (Test-HelperReady $Port $HealthURL) {
        Write-Host "$Name already listening on 127.0.0.1:$Port"
        return
    }
    if (Test-Port $Port) {
        throw "$Name has a listener on 127.0.0.1:$Port, but its health check failed. Stop the stale classifier process and retry."
    }

    $job = Start-Job -Name $Name -ScriptBlock {
        param([string]$CommandText)
        Invoke-Expression $CommandText
    } -ArgumentList $Command

    $script:HelperJobs += $job
	$deadline = (Get-Date).AddMinutes(10)
	while ((Get-Date) -lt $deadline -and -not (Test-HelperReady $Port $HealthURL)) {
		Start-Sleep -Milliseconds 500
		if ($job.State -in @("Failed", "Completed", "Stopped")) {
			$output = Receive-Job -Job $job -ErrorVariable jobErrors -ErrorAction SilentlyContinue | Out-String
			$errorText = $jobErrors | Out-String
			$reason = $job.ChildJobs[0].JobStateInfo.Reason
			throw "$Name failed to start.`n$output$errorText$reason"
		}
	}

	if (-not (Test-HelperReady $Port $HealthURL)) {
		$output = Receive-Job -Job $job -Keep -ErrorAction SilentlyContinue | Out-String
		throw "$Name did not become healthy on 127.0.0.1:$Port within 10 minutes. $output"
    }

    Write-Host "Started $Name on 127.0.0.1:$Port"
}

Set-Location $Root

try {
	$installedModelReady = (Test-Path -LiteralPath (Join-Path $DebertaModelDir "config.json") -PathType Leaf)
	$useRealDeberta = $RealDeberta -or (-not $MockDeberta -and $installedModelReady)

	if ($useRealDeberta) {
        $classifier = Join-Path $Root "semantic-classifier\deberta\start-semantic-classifier.ps1"
        Start-HelperJob `
            -Name "ThemistoRealDebertaDev" `
            -Port 17177 `
			-HealthURL "http://127.0.0.1:17177/healthz" `
			-Command "powershell -NoProfile -ExecutionPolicy Bypass -File `"$classifier`" -ClassifierDir `"$Root\semantic-classifier\deberta`" -ModelDir `"$DebertaModelDir`""
		Write-Host "Using installed DeBERTa model at $DebertaModelDir"
    } else {
		if (-not $MockDeberta) {
			Write-Warning "No installed DeBERTa model was found at $DebertaModelDir. Falling back to the mock classifier. Pass -MockDeberta to make this explicit."
		}
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
