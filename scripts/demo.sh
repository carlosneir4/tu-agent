#!/usr/bin/env bash
# demo.sh — Automates Claims A and B from the tu-agent demo.
#
# Claim A (token savings): runs 5 tasks with Claude and records total tokens.
# Claim B (routing savings): runs same 5 tasks with Qwen and compares cost.
#
# Prerequisites:
#   export ANTHROPIC_API_KEY="..."
#   export QWEN_API_KEY="..."
#   LM Studio running at http://192.168.1.31:1234 (or update .tu-agent/config.yaml)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$REPO_ROOT/tu-agent"
RESULTS_DIR="$REPO_ROOT/scripts/demo-results"
TASKS_FILE="$SCRIPT_DIR/tasks.txt"

check_prereqs() {
  if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
    echo "ERROR: ANTHROPIC_API_KEY is not set" >&2
    exit 1
  fi
  if [[ -z "${QWEN_API_KEY:-}" ]]; then
    echo "ERROR: QWEN_API_KEY is not set" >&2
    exit 1
  fi
  if [[ ! -f "$TASKS_FILE" ]]; then
    echo "ERROR: tasks file not found at $TASKS_FILE" >&2
    exit 1
  fi
}

build_binary() {
  echo "Building tu-agent..."
  cd "$REPO_ROOT"
  go build -tags sqlite_fts5 -o "$BINARY" ./cmd/tu-agent
  echo "Binary: $BINARY"
}

run_tasks() {
  local provider="$1"
  local tel_path="$2"
  local label="$3"

  echo ""
  echo "=== $label ==="
  echo ""

  local n=0
  while IFS= read -r task; do
    [[ -z "$task" ]] && continue
    n=$((n + 1))
    echo "  Task $n: $task"
    "$BINARY" run \
      --provider "$provider" \
      --task "$task" \
      --telemetry "$tel_path" \
      > "$RESULTS_DIR/task-${provider}-${n}.txt" 2>&1
    echo "    done (output: $RESULTS_DIR/task-${provider}-${n}.txt)"
  done < "$TASKS_FILE"
}

main() {
  check_prereqs
  build_binary

  mkdir -p "$RESULTS_DIR"

  BASELINE_TEL="$RESULTS_DIR/baseline-claude.jsonl"
  ROUTED_TEL="$RESULTS_DIR/routed-qwen.jsonl"
  rm -f "$BASELINE_TEL" "$ROUTED_TEL"

  run_tasks "claude"  "$BASELINE_TEL" "Phase 1: Baseline (all-Claude)"
  run_tasks "qwen"    "$ROUTED_TEL"   "Phase 2: Routed (Qwen local)"

  echo ""
  echo "=== Claim B: Routing Cost Savings ==="
  "$BINARY" bench --baseline "$BASELINE_TEL" --compare "$ROUTED_TEL"

  echo ""
  echo "=== Claim A: Token Count (tu-agent baseline) ==="
  echo ""
  "$BINARY" bench --baseline "$BASELINE_TEL" --compare "$BASELINE_TEL"
  echo ""
  echo "Note: 'Total tokens' in Baseline above is your tu-agent token count for Claim A."
  echo "Compare it to your Claude Code session token log (target: >=30% reduction)."
  echo ""
  echo "Raw telemetry:"
  echo "  Baseline : $BASELINE_TEL"
  echo "  Routed   : $ROUTED_TEL"
  echo ""
  echo "Individual task outputs in: $RESULTS_DIR/"
}

main "$@"
