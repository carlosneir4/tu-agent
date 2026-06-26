#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh
require jq

# wrap_prompt is sourced from run.sh; it must append the footer to a task prompt.
source ./run.sh --source-only

wrapped="$(wrap_prompt "What does X do?")"
case "$wrapped" in
  *"What does X do?"*"=== BENCH REPORT ==="*) echo "ok: prompt wrapped with footer" ;;
  *) echo "FAIL: footer not appended"; exit 1 ;;
esac

# build_cmd must produce a claude headless invocation with stream-json + model + verbose.
cmd="$(build_cmd "MODELX" 12 "PROMPT")"
for needle in "claude" "-p" "--output-format stream-json" "--verbose" "--model MODELX" "--max-turns 12" "--strict-mcp-config"; do
  case "$cmd" in *"$needle"*) echo "ok: cmd has $needle" ;; *) echo "FAIL: cmd missing $needle: $cmd"; exit 1 ;; esac
done

assert_eq "--mcp-config .mcp.json" "$(mcp_flag learned)" "mcp_flag learned"
assert_eq "--mcp-config .mcp.json" "$(mcp_flag graph)"   "mcp_flag graph"
assert_eq ""                       "$(mcp_flag cold)"    "mcp_flag cold"

# cold_shim_dir must produce a tu-agent that shadows the real binary (exit 127),
# so the cold condition cannot reach the graph via the CLI.
shim="$(cold_shim_dir)"
rc=0; PATH="$shim:$PATH" tu-agent graph status >/dev/null 2>&1 || rc=$?
assert_eq 127 "$rc" "cold shim shadows tu-agent (exit 127)"
[ -x "$shim/tu-agent" ] && echo "ok: shim tu-agent is executable" || { echo "FAIL: shim not executable"; exit 1; }
rm -rf "$shim"
