param(
    [string]$ListenPrefix = "http://127.0.0.1:17177/"
)

$ErrorActionPreference = "Stop"

$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add($ListenPrefix)
$listener.Start()

Write-Host "Mock DeBERTa classifier listening on $ListenPrefix"
Write-Host "POST /v1/classify with prompt_text. Press Ctrl+C to stop."

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

        $reader = [System.IO.StreamReader]::new($req.InputStream, $req.ContentEncoding)
        $raw = $reader.ReadToEnd()
        $reader.Close()

        $prompt = ""
        try {
            $body = $raw | ConvertFrom-Json
            $prompt = [string]$body.prompt_text
        } catch {
            $prompt = ""
        }

        $lower = $prompt.ToLowerInvariant()
        if ($lower -match "api key|aws secret|secret key|private key|password|token|credential|source code|customer data|employee data|regulated data|ssn|credit card|dump this database|upload this repository") {
            $payload = @{
                decision = "block"
                confidence = 0.92
                reason = "The prompt appears to expose sensitive corporate data or credentials."
                category = "corporate_data_risk"
                source = "local_deberta"
                ambiguous = $false
            }
        } elseif ($lower -match "summarize|rewrite|brainstorm|draft|explain|debug without secrets|public documentation") {
            $payload = @{
                decision = "forward"
                confidence = 0.88
                reason = "The prompt asks for ordinary business assistance without sensitive data exposure."
                category = "approved_business_ai"
                source = "local_deberta"
                ambiguous = $false
            }
        } else {
            $payload = @{
                decision = "alert"
                confidence = 0.54
                reason = "The local classifier is uncertain."
                category = "ambiguous_corporate_ai_request"
                source = "local_deberta"
                ambiguous = $true
            }
        }

        $json = $payload | ConvertTo-Json -Depth 6
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
        $res.StatusCode = 200
        $res.ContentType = "application/json"
        $res.OutputStream.Write($bytes, 0, $bytes.Length)
        $res.Close()
    }
} finally {
    $listener.Stop()
    $listener.Close()
}
