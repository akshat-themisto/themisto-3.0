#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

pattern='openai|anthropic|google|claude|gemini|acme_ai'
files=(backend/internal/api/ai_ledger_handler.go dashboard/src/pages/AILedger.jsx)
for file in backend/internal/ailedger/*.go; do
  [[ "$file" == *_test.go ]] || files+=("$file")
done

violations="$(rg -n -i "$pattern" "${files[@]}" --glob '!*_test.go' || true)"
if [[ -n "$violations" ]]; then
  echo "vendor literals found outside connector adapters or endpoint catalog:" >&2
  echo "$violations" >&2
  exit 1
fi
echo "AI Ledger vendor-neutral core guard passed."
