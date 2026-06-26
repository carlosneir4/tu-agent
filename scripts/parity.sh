#!/usr/bin/env bash
# parity.sh — run the same prompt against claude and qwen, diff the outputs.
#
# Usage:
#   ./scripts/parity.sh "explain how the cache-key filter works"
#   ./scripts/parity.sh --providers claude,qwen "your prompt here"
#
# Requirements:
#   ANTHROPIC_API_KEY and QWEN_API_KEY must be set in your environment.
#   tu-agent binary must be on PATH or in ./bin/.
#
# Outputs are saved to out/parity-<provider>-<timestamp>.txt for archiving.
# See docs/week-3-parity.md for how to interpret the results.

set -euo pipefail

# ---------- defaults ----------
PROVIDERS="claude,qwen"
BINARY="${BINARY:-$(command -v tu-agent 2>/dev/null || echo ./bin/tu-agent)}"
OUTDIR="out"

# ---------- arg parsing ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --providers)
      PROVIDERS="$2"; shift 2 ;;
    --providers=*)
      PROVIDERS="${1#*=}"; shift ;;
    --binary)
      BINARY="$2"; shift 2 ;;
    --binary=*)
      BINARY="${1#*=}"; shift ;;
    --help|-h)
      sed -n '2,14p' "$0" | sed 's/^# \?//'
      exit 0 ;;
    --)
      shift; break ;;
    -*)
      echo "Unknown flag: $1" >&2; exit 1 ;;
    *)
      break ;;
  esac
done

PROMPT="${*}"
if [[ -z "$PROMPT" ]]; then
  echo "Usage: $0 [--providers claude,qwen] \"your prompt here\"" >&2
  exit 1
fi

if [[ ! -x "$BINARY" ]]; then
  echo "tu-agent binary not found or not executable: $BINARY" >&2
  echo "Run: go build -tags sqlite_fts5 -o bin/tu-agent ./cmd/tu-agent" >&2
  exit 1
fi

mkdir -p "$OUTDIR"

TS=$(date +%Y%m%dT%H%M%S)
declare -A OUTFILES

# ---------- run each provider ----------
IFS=',' read -ra PROVIDER_LIST <<< "$PROVIDERS"
for PROV in "${PROVIDER_LIST[@]}"; do
  OUTFILE="$OUTDIR/parity-${PROV}-${TS}.txt"
  OUTFILES[$PROV]="$OUTFILE"
  echo "=== Running provider: $PROV ==="
  {
    echo "provider: $PROV"
    echo "prompt: $PROMPT"
    echo "timestamp: $TS"
    echo "---"
    # Send the prompt via stdin; /exit terminates the session.
    printf '%s\n/exit\n' "$PROMPT" | "$BINARY" chat --provider="$PROV" 2>/dev/null | grep -v '^tu-agent chat'
  } | tee "$OUTFILE"
  echo ""
done

# ---------- diff (only meaningful when exactly 2 providers) ----------
if [[ ${#PROVIDER_LIST[@]} -eq 2 ]]; then
  P1="${PROVIDER_LIST[0]}"
  P2="${PROVIDER_LIST[1]}"
  echo "=== Diff: $P1 vs $P2 ==="
  diff -u --label "$P1" --label "$P2" \
    "${OUTFILES[$P1]}" "${OUTFILES[$P2]}" || true
  echo ""
  echo "Full outputs saved:"
  echo "  ${OUTFILES[$P1]}"
  echo "  ${OUTFILES[$P2]}"
fi
