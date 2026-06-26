#!/usr/bin/env bash
# graph-bench.sh — A/B token benchmark for the knowledge graph (spec: "Token
# measurement"). Runs each task in scripts/graph-bench-tasks.txt twice:
#   baseline : the task alone (agent must explore via file tools)
#   graph    : the same task with `tu-agent graph context <target>` prepended
#              (agent reads only the pointed line ranges)
# Then reports the aggregate token + cost delta via `tu-agent bench`.
#
# Usage:
#   scripts/graph-bench.sh [--provider local] [--tasks FILE] [--bin ./tu-agent]
#
# Requires: a built graph (`tu-agent graph build`) and a reachable provider.
set -euo pipefail

PROVIDER="local"
TASKS="scripts/graph-bench-tasks.txt"
BIN="${TU_AGENT_BIN:-tu-agent}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --provider) PROVIDER="$2"; shift 2 ;;
    --tasks)    TASKS="$2";    shift 2 ;;
    --bin)      BIN="$2";      shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

OUTDIR="$(mktemp -d)"
BASELINE="$OUTDIR/baseline.jsonl"
GRAPH="$OUTDIR/graph.jsonl"
: > "$BASELINE"
: > "$GRAPH"

echo "graph-bench: provider=$PROVIDER tasks=$TASKS out=$OUTDIR"

while IFS= read -r line; do
  [[ -z "$line" || "$line" == \#* ]] && continue
  target="$(echo "${line%%|*}" | xargs)"
  prompt="$(echo "${line#*|}" | xargs)"
  [[ -z "$target" || -z "$prompt" ]] && continue

  echo "  task: $target"

  # Baseline: prompt alone.
  "$BIN" run --provider "$PROVIDER" --telemetry "$BASELINE" \
    --task "$prompt" >/dev/null

  # Graph: prepend the compact context block for $target.
  ctx="$("$BIN" graph context "$target" || true)"
  "$BIN" run --provider "$PROVIDER" --telemetry "$GRAPH" \
    --task "Relevant context from the code graph (pointers only):

$ctx

Task: $prompt" >/dev/null

done < "$TASKS"

echo
"$BIN" bench --baseline "$BASELINE" --compare "$GRAPH"
echo
echo "raw telemetry: $BASELINE  /  $GRAPH"
