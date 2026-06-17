param(
    [string]$ListenPrefix = "http://127.0.0.1:17178/",
    [string]$OllamaURL = "http://127.0.0.1:11434/api/chat",
    [string]$Model = "qwen3.5:397b-cloud"
)

$ErrorActionPreference = "Stop"

$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add($ListenPrefix)
$listener.Start()

Write-Host "Real Qwen classifier listening on $ListenPrefix"
Write-Host "Using Ollama model $Model via $OllamaURL"
Write-Host "POST /v1/classify with prompt_text. Press Ctrl+C to stop."

function New-QwenPrompt($InputBody) {
    $policy = [string]$InputBody.policy
    if ([string]::IsNullOrWhiteSpace($policy)) {
        $policy = "Block credentials, secrets, source code, customer data, employee data, regulated records, production logs, and unsanctioned AI use. Allow approved business assistance when sensitive data has been removed."
    }

    return @"
You are Themisto Labs' prompt policy classifier.

Classify the user's AI prompt using this policy:
$policy

Return only compact JSON. No markdown. No prose outside JSON.

Allowed decisions:
- "block": clear sensitive data exposure, credential leakage, source code leakage, regulated data exposure, unsanctioned AI use, or policy violation.
- "alert": uncertain or risky but not clearly block.
- "forward": approved business assistance, drafting, explanation, summarization, brainstorming, or benign use.

Required JSON shape:
{
  "decision": "block|alert|forward",
  "confidence": 0.0,
  "reason": "short plain-English reason",
  "category": "corporate_data_risk|approved_business_ai|ambiguous_corporate_ai_request|benign|other_policy",
  "ambiguous": false
}

User prompt:
$($InputBody.prompt_text)
"@
}

function ConvertTo-ClassifierResult($RawText) {
    $clean = ([string]$RawText).Trim()
    if ($clean.StartsWith('```')) {
        $clean = $clean -replace '^```(?:json)?\s*', ''
        $clean = $clean -replace '\s*```$', ''
        $clean = $clean.Trim()
    }

    try {
        $parsed = $clean | ConvertFrom-Json
    } catch {
        return @{
            decision = "alert"
            confidence = 0.5
            reason = "Qwen returned a non-JSON classifier response."
            category = "ambiguous_academic_request"
            source = "gateway_qwen"
            ambiguous = $true
        }
    }

    $decision = [string]$parsed.decision
    if ($decision -notin @("block", "alert", "forward")) {
        $decision = "alert"
    }

    $confidence = 0.5
    if ($null -ne $parsed.confidence) {
        $confidence = [double]$parsed.confidence
    }
    if ($confidence -lt 0) { $confidence = 0 }
    if ($confidence -gt 1) { $confidence = 1 }

    $reason = [string]$parsed.reason
    if ([string]::IsNullOrWhiteSpace($reason)) {
        $reason = "Qwen classified the prompt."
    }

    $category = [string]$parsed.category
    if ([string]::IsNullOrWhiteSpace($category)) {
        $category = "other_policy"
    }

    $ambiguous = $false
    if ($null -ne $parsed.ambiguous) {
        $ambiguous = [bool]$parsed.ambiguous
    } elseif ($decision -eq "alert") {
        $ambiguous = $true
    }

    return @{
        decision = $decision
        confidence = $confidence
        reason = $reason
        category = $category
        source = "gateway_qwen"
        ambiguous = $ambiguous
    }
}

try {
    while ($listener.IsListening) {
        $ctx = $listener.GetContext()
        $req = $ctx.Request
        $res = $ctx.Response

        if ($req.HttpMethod -ne "POST" -or $req.Url.AbsolutePath -ne "/v1/classify") {
            $res.StatusCode = 404
            $res.Close()
            continue
        }

        try {
            $reader = [System.IO.StreamReader]::new($req.InputStream, $req.ContentEncoding)
            $raw = $reader.ReadToEnd()
            $reader.Close()
            $inputBody = $raw | ConvertFrom-Json

            $ollamaBody = @{
                model = $Model
                stream = $false
                format = "json"
                options = @{
                    temperature = 0
                    num_predict = 180
                }
                messages = @(
                    @{
                        role = "user"
                        content = (New-QwenPrompt $inputBody)
                    }
                )
            } | ConvertTo-Json -Depth 10

            $ollamaResp = Invoke-RestMethod -Uri $OllamaURL -Method Post -ContentType "application/json" -Body $ollamaBody -TimeoutSec 60
            $payload = ConvertTo-ClassifierResult $ollamaResp.message.content

            $json = $payload | ConvertTo-Json -Depth 6
            $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
            $res.StatusCode = 200
            $res.ContentType = "application/json"
            $res.OutputStream.Write($bytes, 0, $bytes.Length)
        } catch {
            $payload = @{
                error = "qwen_classifier_error"
                message = $_.Exception.Message
            } | ConvertTo-Json -Depth 4
            $bytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
            $res.StatusCode = 502
            $res.ContentType = "application/json"
            $res.OutputStream.Write($bytes, 0, $bytes.Length)
        } finally {
            $res.Close()
        }
    }
} finally {
    $listener.Stop()
    $listener.Close()
}
