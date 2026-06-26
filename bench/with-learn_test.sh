#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh

repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
mkdir -p "$repo/.claude/skills/foo" "$repo/.tu-agent"
echo x > "$repo/.claude/skills/foo/SKILL.md"
echo x > "$repo/.mcp.json"
echo x > "$repo/.tu-agent/graph.db"

present() { [ -e "$repo/$1" ] && echo present || echo absent; }

ROOT="$repo" ./with-learn.sh cold
assert_eq "absent" "$(present .claude/skills)"     "cold: skills hidden"
assert_eq "absent" "$(present .mcp.json)"          "cold: mcp hidden"
assert_eq "absent" "$(present .tu-agent/graph.db)" "cold: graph hidden"

ROOT="$repo" ./with-learn.sh graph
assert_eq "absent"  "$(present .claude/skills)"     "graph: skills hidden"
assert_eq "present" "$(present .mcp.json)"          "graph: mcp present"
assert_eq "present" "$(present .tu-agent/graph.db)" "graph: graph present"

ROOT="$repo" ./with-learn.sh learned
assert_eq "present" "$(present .claude/skills/foo/SKILL.md)" "learned: skills present"
assert_eq "present" "$(present .mcp.json)"                   "learned: mcp present"

ROOT="$repo" ./with-learn.sh cold
ROOT="$repo" ./with-learn.sh restore
assert_eq "present" "$(present .claude/skills/foo/SKILL.md)" "restore: skills back"
assert_eq "present" "$(present .mcp.json)"                   "restore: mcp back"
assert_eq "present" "$(present .tu-agent/graph.db)"          "restore: graph back"
