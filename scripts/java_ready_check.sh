#!/usr/bin/env bash
# Java-readiness check (WS4): deterministic, zero model calls.
#
# Exercises the full no-API-key tu-agent path end to end so the first run on a
# real Java repo is not the first time the pipeline runs. Run from the root of
# the Java repo under test.
#
#   scripts/java_ready_check.sh             # check the current directory
#   scripts/java_ready_check.sh --strict    # treat extractor warnings as failures (CI/fixture)
#   CONCEPT_ROOT=com.acme.shop scripts/java_ready_check.sh
#   TU_AGENT=/path/to/tu-agent scripts/java_ready_check.sh
#
# Side effects: builds the graph (.tu-agent/) and runs `learn --skip-llm`, which
# writes .claude/skills/<concept>/SKILL.md and a CLAUDE.md knowledge block in
# the current directory — the same artifacts a real onboarding produces. Run it
# where you want those.
set -euo pipefail

strict=0
[ "${1:-}" = "--strict" ] && strict=1

TU="${TU_AGENT:-tu-agent}"

fail() { echo "FAIL: $*" >&2; exit 1; }
warn() { if [ "$strict" = 1 ]; then fail "$*"; else echo "WARN: $*" >&2; fi; }
ok()   { echo "ok: $*"; }

command -v "$TU"   >/dev/null || fail "$TU not on PATH (set TU_AGENT=/path/to/tu-agent)"
command -v jq      >/dev/null || fail "jq is required"
command -v sqlite3 >/dev/null || fail "sqlite3 is required"

# 1. graph build succeeds with no failed files
"$TU" graph build . >/dev/null || fail "graph build failed"
status="$("$TU" graph status)"
echo "$status" | grep -qE '\(failed=0\)'        || fail "graph has failed files: $status"
echo "$status" | grep -qE "extractor=[^ ]+"    || fail "no extractor version in: $status"
files="$(echo "$status" | sed -n 's/^files=\([0-9]*\).*/\1/p')"
[ "${files:-0}" -gt 0 ]                         || fail "graph has zero files: $status"
ok "graph build ($status)"

# 2. semantic extractor features active on this repo
db=".tu-agent/graph.db"
overrides="$(sqlite3 "$db" "SELECT count(*) FROM edges WHERE kind='overrides';")"
external="$(sqlite3 "$db" "SELECT count(*) FROM nodes WHERE kind='external';")"
[ "$overrides" -gt 0 ] || warn "no overrides edges (no @Override methods resolved)"
[ "$external"  -gt 0 ] || warn "no external:: stub nodes (no refs to compiled libs resolved)"
ok "overrides=$overrides external=$external"

# 3. learn --skip-llm: zero model calls, deterministic artifacts
tel=".tu-agent/telemetry.jsonl"
model_calls() { if [ -f "$tel" ]; then jq -s '[.[]|select((.event//"")=="")]|length' "$tel"; else echo 0; fi; }
before="$(model_calls)"
if [ -n "${CONCEPT_ROOT:-}" ]; then
  "$TU" learn --skip-llm --concept-root "$CONCEPT_ROOT" . >/dev/null || fail "learn --skip-llm failed"
else
  "$TU" learn --skip-llm . >/dev/null || fail "learn --skip-llm failed"
fi
after="$(model_calls)"
[ "$after" -eq "$before" ] || fail "learn --skip-llm made $((after - before)) model call(s); expected 0"
{ [ -f CLAUDE.md ] && grep -q "tu-agent:knowledge" CLAUDE.md; } || fail "no knowledge block in CLAUDE.md"
cards="$(find .claude/skills -name SKILL.md 2>/dev/null | wc -l | tr -d ' ')"
[ "${cards:-0}" -gt 0 ] || fail "no concept cards under .claude/skills/"
ok "learn --skip-llm: 0 model calls, $cards concept card(s), knowledge block"

# 4. concepts prints a deterministic index
"$TU" concepts . | grep -q '^name:' || fail "concepts produced no cards"
ok "concepts index prints cards"

# 5. test gaps --json returns a JSON array
"$TU" test gaps --json | jq -e 'type=="array"' >/dev/null || fail "test gaps --json is not a JSON array"
ok "test gaps --json returns an array"

# 6. mcp --list exposes every required tool
list="$("$TU" mcp --list)"
# Keep in sync with mcpToolNames in cmd/tu-agent/mcp.go — TestMCPListFlag guards the binary side.
for t in get_impact get_context find_symbol get_concept get_traits get_flow \
         mem_save mem_search mem_recent test_gaps test_scaffold; do
  echo "$list" | grep -qx "$t" || fail "mcp tool missing: $t"
done
ok "mcp --list exposes all required tools"

echo "PASS: java-readiness check"
