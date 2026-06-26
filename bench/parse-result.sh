#!/usr/bin/env bash
# Reads a Claude Code stream-json log on stdin, emits one compact results JSON
# row on stdout. Args: TASK_ID CONDITION REP
set -euo pipefail
task_id="${1:?task_id}"; condition="${2:?condition}"; rep="${3:?rep}"

jq -s -c \
  --arg task "$task_id" --arg cond "$condition" --argjson rep "$rep" '
  [ .[] | select(.type=="assistant") | .message.content[]?
          | select(.type=="tool_use") | .name ] as $tools
  | ([ .[] | select(.type=="result") ] | last) as $r
  | ($r.usage // {}) as $u
  | {
      task_id: $task, condition: $cond, rep: $rep,
      input_tokens:          ($u.input_tokens // 0),
      cache_read_tokens:     ($u.cache_read_input_tokens // 0),
      cache_creation_tokens: ($u.cache_creation_input_tokens // 0),
      output_tokens:         ($u.output_tokens // 0),
      total_input: (($u.input_tokens // 0)
                    + ($u.cache_read_input_tokens // 0)
                    + ($u.cache_creation_input_tokens // 0)),
      cost_usd:    ($r.total_cost_usd // 0),
      duration_ms: ($r.duration_ms // 0),
      num_turns:   ($r.num_turns // 0),
      files_read:  ([ $tools[] | select(. == "Read") ] | length),
      searches:    ([ $tools[] | select(. == "Grep" or . == "Glob") ] | length),
      graph_calls: ([ $tools[] | select(startswith("mcp__") and (test("graph"))) ] | length),
      answer_text: ($r.result // "")
    }'
