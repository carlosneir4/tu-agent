#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh
require jq

row="$(./parse-result.sh task-1 learned 2 < testdata/sample-stream.jsonl)"

assert_eq "task-1"  "$(jq -r .task_id        <<<"$row")" "task_id"
assert_eq "learned" "$(jq -r .condition      <<<"$row")" "condition"
assert_eq "2"       "$(jq -r .rep            <<<"$row")" "rep"
assert_eq "9500"    "$(jq -r .total_input    <<<"$row")" "total_input = 1000+500+8000"
assert_eq "200"     "$(jq -r .output_tokens  <<<"$row")" "output_tokens"
assert_eq "0.12"    "$(jq -r .cost_usd       <<<"$row")" "cost_usd"
assert_eq "4200"    "$(jq -r .duration_ms    <<<"$row")" "duration_ms"
assert_eq "3"       "$(jq -r .num_turns      <<<"$row")" "num_turns"
assert_eq "1"       "$(jq -r .files_read     <<<"$row")" "files_read = one Read"
assert_eq "1"       "$(jq -r .searches       <<<"$row")" "searches = one Grep"
assert_eq "1"       "$(jq -r .graph_calls    <<<"$row")" "graph_calls = one mcp graph tool"
