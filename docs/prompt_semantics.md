# Prompt Semantics

Themisto Labs uses a tiered prompt decision path:

1. The endpoint agent runs deterministic DLP and policy checks.
2. If prompt semantics is enabled, the agent calls the bundled local DeBERTa classifier at `http://127.0.0.1:17177/v1/classify`.
3. If the local result is ambiguous or below the configured confidence threshold, the agent calls the gateway endpoint `POST /v1/prompt/semantic-evaluate`.
4. The gateway endpoint forwards the request to the centrally hosted Qwen classifier configured by `QWEN_CLASSIFIER_URL`. If `QWEN_CLASSIFIER_API_KEY` is set, the gateway sends it as a bearer token.

The local classifier is expected to ship with each installed agent package. It should be a low-latency DeBERTa-based service bound to loopback only.

The Chromium prompt extension also performs lightweight destination detection.
Known AI domains are always treated as AI tools, and unknown pages are treated
as AI tools when the host, page text, prompt field, and send controls look like
an AI/chat composer. Dynamically detected AI pages are sent to the agent with
`service_category=ai_llm`, `vendor=unknown_ai`, and explanatory metadata. Admin
policies can target these sites with `service_category == ai_llm`.

## Local DeBERTa Sidecar

The local classifier now uses a separate loopback port so it does not collide with the desktop/status API:

- Prompt capture API: `http://127.0.0.1:17175`
- Agent desktop/status API: `http://127.0.0.1:17176`
- Local DeBERTa classifier: `http://127.0.0.1:17177/v1/classify`

Windows install layout:

- Classifier launcher and server files: `C:\Program Files\Themisto\classifier`
- Model files: `C:\ProgramData\Themisto\models\deberta`
- Logs: `C:\ProgramData\Themisto\logs\semantic-classifier.log`
- Scheduled task: `ThemistoSemanticClassifier`

The Go installer embeds `cmd/installer/assets/classifier`, and the bundle scripts copy `semantic-classifier/deberta` into `classifier/` for agent-only install bundles.

For production, place the DeBERTa model directory into `cmd/installer/assets/classifier/model` before building the installer, or place it in `C:\ProgramData\Themisto\models\deberta` during managed deployment. The included `download-model.ps1` is a development helper, not an offline production artifact.

The default local model is `microsoft/deberta-v3-small`. This is a normal base
DeBERTa model, not a zero-shot/NLI classifier. The sidecar therefore does not
pretend that the raw base model can classify prompts by itself; it loads the
model for runtime validation and uses the local corporate policy scorer for
clear endpoint DLP indicators. Ambiguous prompts are still escalated to the
gateway classifier path.

Development commands:

```powershell
powershell -ExecutionPolicy Bypass -File .\semantic-classifier\deberta\download-model.ps1
powershell -ExecutionPolicy Bypass -File .\semantic-classifier\deberta\start-semantic-classifier.ps1 `
  -ClassifierDir ".\semantic-classifier\deberta" `
  -ModelDir "C:\ProgramData\Themisto\models\deberta"
```

For the normal agent dev loop, use the wrapper instead. It starts the local
classifier helper and then runs the dev agent in the foreground:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-agent-dev.ps1
```

To use the real downloaded DeBERTa model instead of the mock classifier:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-agent-dev.ps1 -RealDeberta
```

## Request

```json
{
  "prompt_text": "Here is our AWS secret key. Help me debug this deployment.",
  "policy": "Block secrets, source code, regulated data, and unsanctioned AI use. Allow approved business assistance...",
  "surface": "browser_chromium",
  "app_name": "ChatGPT",
  "destination_host": "chatgpt.com",
  "destination_path": "/",
  "vendor": "openai",
  "service_category": "ai_llm",
  "metadata": {}
}
```

## Response

```json
{
  "decision": "block",
  "confidence": 0.91,
  "reason": "The prompt appears to include sensitive credentials or corporate data.",
  "category": "corporate_data_risk",
  "source": "local_deberta",
  "ambiguous": false
}
```

Valid `decision` values are `forward`, `alert`, and `block`.

## Defaults

- Local DeBERTa timeout: `250ms`
- Gateway Qwen timeout: `900ms`
- Block threshold: `0.86`
- Alert threshold: `0.68`
- Ambiguous escalation threshold: `0.58`

If semantic evaluation fails, the agent keeps the existing deterministic policy decision and fails open for the semantic layer.

## Gateway Qwen Configuration

Set these on the gateway host:

```env
PROMPT_SEMANTICS_ENABLED=true
QWEN_CLASSIFIER_URL=https://your-qwen-classifier.example.com/v1/classify
QWEN_CLASSIFIER_API_KEY=replace_me
QWEN_CLASSIFIER_TIMEOUT=900ms
```

The Qwen classifier must accept the same request JSON and return the same response JSON shown above.

## Local Fallback Test

To test the gateway fallback without a real Qwen service, run the agent dev
wrapper with mock Qwen enabled:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-agent-dev.ps1 -MockQwen
```

When the gateway runs in Docker, set:

```env
PROMPT_SEMANTICS_ENABLED=true
QWEN_CLASSIFIER_URL=http://host.docker.internal:17178/v1/classify
QWEN_CLASSIFIER_API_KEY=
```

Then use a prompt that the local mock DeBERTa classifier treats as ambiguous,
for example: `Can you help rewrite this internal paragraph?`
