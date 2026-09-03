# Prompt semantics capability status

Themisto's production enforcement baseline is deterministic endpoint DLP and policy evaluation. It remains functional when semantic classification is disabled, unavailable, slow, or malformed.

The agent contains an optional client for a loopback-only endpoint service at `http://127.0.0.1:17177/v1/classify`. Prompt text and locally extracted attachment text may be sent to that local process. The semantic client has a bounded timeout and, on failure, returns control to the existing deterministic decision path.

The repository does not contain a complete production semantic classifier. In particular, it does not ship the required ONNX inference runtime, tokenizer, approved model artifact, and production classifier server as one supported runtime. The files under `semantic-classifier/` and model-manager code are scaffolding/development assets, not evidence of a complete LLM evaluator.

Prompt content is not sent to the backend or gateway as semantic fallback. The legacy gateway fallback configuration is ignored by the endpoint client, defaults to disabled in installers, and the gateway does not register a central semantic-evaluation route. Reintroducing any cloud or central model requires a separate explicit product, privacy, and security decision.

## Local contract

Request:

```json
{
  "prompt_text": "content processed on this endpoint",
  "policy": "local policy text",
  "surface": "browser_chromium",
  "app_name": "AI product",
  "destination_host": "example.ai",
  "vendor": "vendor_key",
  "service_category": "ai_llm",
  "metadata": {}
}
```

Response decisions are `forward`, `alert`, or `block`, with optional confidence, reason, category, source, and ambiguity fields. The service must bind to loopback only. It must not log prompts, extracted text, credentials, or request bodies.

Until the runtime is completed and approved, validate deterministic DLP independently and treat semantic health as `disabled`, `unavailable`, or degraded—not as a complete evaluator.
