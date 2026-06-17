import argparse
import http.client
import json
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


DEFAULT_POLICY = (
    "Block credentials, secrets, source code, customer data, employee data, "
    "regulated records, production logs, and unsanctioned AI use. Allow approved "
    "business assistance when sensitive data has been removed."
)
ADAPTER_VERSION = "direct-httpclient-v2"


def build_prompt(body):
    policy = str(body.get("policy") or DEFAULT_POLICY)
    prompt_text = str(body.get("prompt_text") or "")
    return f"""You are Themisto Labs' prompt policy classifier.

Classify the user's AI prompt using this policy:
{policy}

Return only compact JSON. No markdown. No prose outside JSON.

Allowed decisions:
- "block": clear sensitive data exposure, credential leakage, source code leakage, regulated data exposure, unsanctioned AI use, or policy violation.
- "alert": uncertain or risky but not clearly block.
- "forward": approved business assistance, drafting, explanation, summarization, brainstorming, or benign use.

Required JSON shape:
{{
  "decision": "block|alert|forward",
  "confidence": 0.0,
  "reason": "short plain-English reason",
  "category": "corporate_data_risk|approved_business_ai|ambiguous_corporate_ai_request|benign|other_policy",
  "ambiguous": false
}}

User prompt:
{prompt_text}
"""


def extract_json(text):
    text = (text or "").strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*", "", text)
        text = re.sub(r"\s*```$", "", text).strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        match = re.search(r"\{.*\}", text, flags=re.S)
        if match:
            return json.loads(match.group(0))
        raise


def normalize_result(raw, source):
    try:
        parsed = extract_json(raw)
    except Exception:
        return {
            "decision": "alert",
            "confidence": 0.5,
            "reason": "Qwen returned a non-JSON classifier response.",
            "category": "ambiguous_corporate_ai_request",
            "source": source,
            "ambiguous": True,
        }

    decision = str(parsed.get("decision") or "alert").lower()
    if decision not in {"block", "alert", "forward"}:
        decision = "alert"

    try:
        confidence = float(parsed.get("confidence", 0.5))
    except (TypeError, ValueError):
        confidence = 0.5
    confidence = max(0.0, min(1.0, confidence))

    reason = str(parsed.get("reason") or "Qwen classified the prompt.").strip()
    category = str(parsed.get("category") or "other_policy").strip()
    ambiguous = parsed.get("ambiguous")
    if ambiguous is None:
        ambiguous = decision == "alert"

    return {
        "decision": decision,
        "confidence": confidence,
        "reason": reason,
        "category": category,
        "source": source,
        "ambiguous": bool(ambiguous),
    }


def call_ollama(ollama_url, model, source, body):
    payload = {
        "model": model,
        "stream": False,
        "format": "json",
        "options": {"temperature": 0, "num_predict": 180},
        "messages": [{"role": "user", "content": build_prompt(body)}],
    }
    encoded = json.dumps(payload).encode("utf-8")
    parsed = urllib.parse.urlparse(ollama_url)
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port or (443 if parsed.scheme == "https" else 80)
    path = parsed.path or "/api/chat"
    started = time.perf_counter()
    if parsed.scheme == "https":
        conn = http.client.HTTPSConnection(host, port, timeout=90)
    else:
        conn = http.client.HTTPConnection(host, port, timeout=90)
    try:
        conn.request("POST", path, body=encoded, headers={"Content-Type": "application/json"})
        resp = conn.getresponse()
        raw = resp.read().decode("utf-8")
        if resp.status >= 400:
            raise RuntimeError(f"Ollama returned HTTP {resp.status}: {raw}")
        data = json.loads(raw)
    finally:
        conn.close()
    result = normalize_result((data.get("message") or {}).get("content", ""), source)
    result["latency_ms"] = int((time.perf_counter() - started) * 1000)
    return result


class Handler(BaseHTTPRequestHandler):
    ollama_url = "http://127.0.0.1:11434/api/chat"
    model = "qwen3.5:397b-cloud"
    source = "gateway_qwen"

    def do_GET(self):
        if self.path == "/healthz":
            self.write_json({"ok": True, "model": self.model, "version": ADAPTER_VERSION})
            return
        self.send_error(404)

    def do_POST(self):
        if self.path != "/v1/classify":
            self.send_error(404)
            return
        try:
            length = int(self.headers.get("Content-Length") or "0")
            body = json.loads(self.rfile.read(length).decode("utf-8"))
            result = call_ollama(self.ollama_url, self.model, self.source, body)
            self.write_json(result)
        except urllib.error.URLError as exc:
            self.write_json(
                {"error": "ollama_unavailable", "message": str(exc), "version": ADAPTER_VERSION},
                status=502,
            )
        except Exception as exc:
            self.write_json(
                {"error": "qwen_classifier_error", "message": str(exc), "version": ADAPTER_VERSION},
                status=502,
            )

    def write_json(self, payload, status=200):
        data = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt, *args):
        print(fmt % args)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=17178)
    parser.add_argument("--ollama-url", default="http://127.0.0.1:11434/api/chat")
    parser.add_argument("--model", default="qwen3.5:397b-cloud")
    parser.add_argument("--source", default="gateway_qwen")
    args = parser.parse_args()

    Handler.ollama_url = args.ollama_url
    Handler.model = args.model
    Handler.source = args.source

    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"Real Qwen classifier listening on http://{args.host}:{args.port}")
    print(f"Using Ollama model {args.model} via {args.ollama_url}")
    server.serve_forever()


if __name__ == "__main__":
    main()
