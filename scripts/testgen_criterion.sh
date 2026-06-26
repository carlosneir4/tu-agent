#!/usr/bin/env bash
# Manual check: ≥80% of targeted public symbols get a
# passing generated test within 2 repair attempts.
#
# Run from the root of the repo under test, with a provider configured
# (ANTHROPIC_API_KEY or a local model in .tu-agent/config.yaml).
#
# Usage: scripts/testgen_criterion.sh [top-N]   (default 10)
set -euo pipefail

top="${1:-10}"
command -v tu-agent >/dev/null || { echo "tu-agent not on PATH" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

tu-agent graph build >/dev/null

ids=$(tu-agent test gaps --json --top "$top" | jq -r '.[].id')
[ -n "$ids" ] || { echo "no untested public symbols found"; exit 0; }

total=0
pass=0
while IFS= read -r id; do
  total=$((total + 1))
  echo "==> test gen $id"
  if tu-agent test gen "$id" --discard-failing; then
    pass=$((pass + 1))
  fi
done <<< "$ids"

echo
echo "passed: $pass / $total"
awk -v p="$pass" -v t="$total" 'BEGIN { printf "rate: %.0f%% (criterion: >=80%%)\n", (p / t) * 100 }'
