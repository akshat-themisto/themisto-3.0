param(
    [string]$ListenPrefix = "http://127.0.0.1:17178/"
)

$ErrorActionPreference = "Stop"

$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add($ListenPrefix)
$listener.Start()

Write-Host "Mock Qwen classifier listening on $ListenPrefix"
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
        if ($lower -match "draft|summarize|rewrite|brainstorm|explain|debug without secrets|public documentation|policy summary") {
            $payload = @{
                decision = "forward"
                confidence = 0.91
                reason = "Gateway Qwen mock classified this as approved business AI assistance."
                category = "approved_business_ai"
                source = "gateway_qwen"
                ambiguous = $false
            }
        } else {
            $payload = @{
                decision = "block"
                confidence = 0.9
                reason = "Gateway Qwen mock classified this as sensitive corporate data exposure."
                category = "corporate_data_risk"
                source = "gateway_qwen"
                ambiguous = $false
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
