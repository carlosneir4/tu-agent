#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh
require jq

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
res="$tmp/results.jsonl"
cat > "$res" <<'JSON'
{"task_id":"t1","condition":"cold","rep":1,"cost_usd":0.10,"total_input":1000,"output_tokens":200,"duration_ms":5000,"num_turns":8,"files_read":12,"searches":6,"graph_calls":0,"answer_text":"a"}
{"task_id":"t1","condition":"graph","rep":1,"cost_usd":0.06,"total_input":600,"output_tokens":150,"duration_ms":3000,"num_turns":5,"files_read":4,"searches":2,"graph_calls":3,"answer_text":"a"}
{"task_id":"t1","condition":"learned","rep":1,"cost_usd":0.06,"total_input":620,"output_tokens":150,"duration_ms":3000,"num_turns":5,"files_read":4,"searches":2,"graph_calls":3,"answer_text":"a"}
JSON

out="$(./report.sh "$res")"
echo "$out" | grep -q "Cold" && echo "$out" | grep -q "Graph" && echo "$out" | grep -q "Learned" \
  || { echo "FAIL: report missing a condition column" >&2; echo "$out"; exit 1; }
echo "ok: 3 condition columns present"
echo "$out" | grep -qi "cold.*graph" || { echo "FAIL: missing cold->graph delta" >&2; exit 1; }
echo "ok: cold->graph delta present"
echo "$out" | grep -q -- "-40.0%" || { echo "FAIL: cold->graph token delta should be -40.0%" >&2; echo "$out"; exit 1; }
echo "ok: cold->graph token delta arithmetic (-40.0%)"
echo "$out" | grep -qi "graph.*learned" || { echo "FAIL: missing graph->learned delta" >&2; exit 1; }
echo "ok: graph->learned delta present"
