# Prompt Capture Adapters

This directory contains endpoint-side pre-send prompt capture adapters.

## Browser adapters

- `browser/chromium`
- `browser/firefox`
- `browser/safari`

Each browser adapter intercepts prompt submit on supported AI web surfaces, calls the local agent API at `http://127.0.0.1:17175/v1/prompt/evaluate`, and reports adapter action via `POST /v1/prompt/outcome`.

Supported browser surfaces in this branch:

- Claude (`claude.ai`)
- ChatGPT (`chatgpt.com`, `chat.openai.com`)
- Gemini (`gemini.google.com`)
- Grok (`grok.com`, `x.com/.../grok`, `twitter.com/.../grok`, `x.ai`)

Supported outcomes reported by adapters:

- `blocked`
- `allowed`
- `degraded_fail_open`

## Desktop adapters

Desktop capture uses the same local decision API and outcome API.

- macOS Accessibility adapter and Windows UIA adapter should call:
  - `POST /v1/prompt/evaluate`
  - `POST /v1/prompt/outcome`

The local API is loopback-only and enforces the same DLP/policy semantics used by the network proxy path.
