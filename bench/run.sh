#!/usr/bin/env bash
# Run the learn benchmark: each task × condition × reps through Claude Code
# headless, appending one parsed row per run to results.jsonl.
#
# Usage:
#   run.sh --tasks tasks.local.jsonl --model opus --reps 3 --max-turns 30 \
#          --repo /path/to/repo --condition cold|learned
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh

FOOTER_FILE="$BENCH_DIR/prompt-footer.txt"

wrap_prompt() { printf '%s\n\n%s\n' "$1" "$(cat "$FOOTER_FILE")"; }

# mcp_flag CONDITION -> the --mcp-config flag, present only when the graph is
# available (graph and learned conditions); empty for cold.
mcp_flag() {
  case "$1" in
    graph|learned) printf -- '--mcp-config .mcp.json' ;;
    *) printf '' ;;
  esac
}

# cold_shim_dir creates a temp dir holding a `tu-agent` shim that always exits
# 127 ("command not found"). Prepending it to PATH for the cold condition stops
# the agent from reaching the graph via the CLI binary (e.g. `tu-agent graph
# build`/`bridges`/`test gaps`) — the knowledge lives in the binary, not just in
# the .mcp.json/graph.db/skills that with-learn.sh hides. Prints the dir; the
# caller prepends it to PATH and removes it afterward.
cold_shim_dir() {
  local d; d="$(mktemp -d)"
  printf '#!/bin/sh\nexit 127\n' > "$d/tu-agent"
  chmod +x "$d/tu-agent"
  printf '%s' "$d"
}

# build_cmd MODEL MAXTURNS PROMPT -> prints the command line (for tests/logging).
build_cmd() {
  printf 'claude -p %q --output-format stream-json --verbose --model %s --max-turns %s --permission-mode bypassPermissions --strict-mcp-config --mcp-config .mcp.json' \
    "$3" "$1" "$2"
}

run_once() { # MODEL MAXTURNS REPO PROMPT TASK_ID CONDITION REP -> appends row, prints stream to runs/
  local model="$1" maxt="$2" repo="$3" prompt="$4" task="$5" cond="$6" rep="$7"
  local stamp; stamp="$(date +%s)"
  mkdir -p "$BENCH_DIR/runs"
  local log="$BENCH_DIR/runs/${task}.${cond}.${rep}.${stamp}.stream.jsonl"
  # Strip ANTHROPIC_API_KEY so the headless agent uses the Claude Code
  # subscription (login), not pay-per-token API billing. The benchmark is meant
  # to run on Claude Code, not the API.
  #
  # --strict-mcp-config is REQUIRED for a valid ablation: without it, Claude Code
  # also loads user/global MCP servers (e.g. a globally-registered tu-agent-graph),
  # so the "cold" condition silently keeps the graph tools even though with-learn.sh
  # hid the project .mcp.json. --strict-mcp-config makes Claude Code use ONLY the
  # servers passed via --mcp-config: cold (no --mcp-config) → zero MCP (true grep
  # baseline); graph/learned (--mcp-config .mcp.json) → only the project graph.
  #
  # For cold we also shadow the tu-agent binary with a 127 shim on PATH: otherwise
  # the agent reaches the graph via the CLI (tu-agent graph/test), which even
  # rebuilds the hidden graph.db. graph/learned keep the real binary so the MCP
  # server (and any CLI cross-checks) work normally.
  local shim="" runpath=""
  if [ "$cond" = "cold" ]; then
    shim="$(cold_shim_dir)"
    runpath="$shim:$PATH"
  fi
  ( cd "$repo" && env -u ANTHROPIC_API_KEY ${runpath:+PATH="$runpath"} \
      claude -p "$prompt" --output-format stream-json --verbose \
      --model "$model" --max-turns "$maxt" --permission-mode bypassPermissions \
      --strict-mcp-config $(mcp_flag "$cond") ) > "$log"
  [ -n "$shim" ] && rm -rf "$shim"
  "$BENCH_DIR/parse-result.sh" "$task" "$cond" "$rep" < "$log" >> "$BENCH_DIR/results.jsonl"
}

main() {
  local tasks=tasks.local.jsonl model=opus reps=3 maxt=30 repo=. cond=""
  while [ $# -gt 0 ]; do case "$1" in
    --tasks) tasks="$2"; shift 2;;
    --model) model="$2"; shift 2;;
    --reps) reps="$2"; shift 2;;
    --max-turns) maxt="$2"; shift 2;;
    --repo) repo="$2"; shift 2;;
    --condition) cond="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac; done
  [ -n "$cond" ] || { echo "--condition cold|graph|learned required" >&2; exit 2; }
  case "$cond" in cold|graph|learned) ;; *) echo "--condition must be cold|graph|learned" >&2; exit 2;; esac
  require jq; require claude

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    local id base prompt
    id="$(jq -r .id <<<"$line")"
    base="$(jq -r .prompt <<<"$line")"
    prompt="$(wrap_prompt "$base")"
    for rep in $(seq 1 "$reps"); do
      echo ">> $id [$cond] rep $rep"
      # A single failed run (rate limit, timeout, etc.) must not abort the whole
      # matrix — warn and continue so the rest of the conditions still run.
      run_once "$model" "$maxt" "$repo" "$prompt" "$id" "$cond" "$rep" \
        || echo "WARN: run failed, skipped: $id [$cond] rep $rep" >&2
    done
  done < "$tasks"
  echo "done -> bench/results.jsonl"
}

# Allow tests to source helpers without executing.
if [ "${1:-}" = "--source-only" ]; then return 0 2>/dev/null || exit 0; fi
main "$@"
