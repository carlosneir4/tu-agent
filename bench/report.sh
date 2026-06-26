#!/usr/bin/env bash
# Aggregate a results.jsonl into a markdown median/delta table, and emit a blind
# scoring sheet (bench/scoring.csv) for manual quality rating.
# Usage: report.sh [results.jsonl]
set -euo pipefail
# Resolve the results path relative to the caller's CWD before cd-ing.
_caller_cwd="$(pwd)"
cd "$(dirname "$0")"
results="${1:-results.jsonl}"
# If the path is not absolute, make it relative to caller's CWD.
case "$results" in /*) ;; *) results="$_caller_cwd/$results" ;; esac

# median of a numeric field for a given condition.
med() {
  local field="$1" cond="$2"
  jq -rs --arg c "$cond" --arg f "$field" '
    def median: sort | if length==0 then 0
      elif length%2==1 then .[(length-1)/2]
      else ((.[length/2-1] + .[length/2]) / 2) end;
    [ .[] | select(.condition==$c) | .[$f] ] | median
  ' "$results"
}

# row with natural number format (%g strips trailing zeros).
row() { # label field
  printf '| %s | %g | %g | %g |\n' "$1" "$(med "$2" cold)" "$(med "$2" graph)" "$(med "$2" learned)"
}

# rowf with fixed 2-decimal format (for currency values).
rowf() { # label field
  printf '| %s | %.2f | %.2f | %.2f |\n' "$1" "$(med "$2" cold)" "$(med "$2" graph)" "$(med "$2" learned)"
}

echo "## Learn benchmark report"
echo
echo "| Metric | Cold | Graph | Learned |"
echo "|--------|------|-------|---------|"
rowf "Cost (USD)"          cost_usd
row  "Total input tokens"  total_input
row  "Output tokens"       output_tokens
row  "Wall-clock (ms)"     duration_ms
row  "Turns"               num_turns
row  "Files read"          files_read
row  "Searches"            searches
row  "Graph calls"         graph_calls

# pct_delta FROM_VAL TO_VAL -> signed percent ("n/a" when baseline is 0)
pct_delta() { awk -v a="$1" -v b="$2" 'BEGIN{ if (a==0){print "n/a"} else {printf "%+.1f%%", (b-a)/a*100} }'; }

echo
echo "Deltas (median total input tokens):"
printf '  cold -> graph    : %s\n' "$(pct_delta "$(med total_input cold)"  "$(med total_input graph)")"
printf '  graph -> learned : %s\n' "$(pct_delta "$(med total_input graph)" "$(med total_input learned)")"
echo "Deltas (median cost USD):"
printf '  cold -> graph    : %s\n' "$(pct_delta "$(med cost_usd cold)"  "$(med cost_usd graph)")"
printf '  graph -> learned : %s\n' "$(pct_delta "$(med cost_usd graph)" "$(med cost_usd learned)")"
echo
echo "_Quality is rated manually: fill quality_1to5 in bench/scoring.csv (condition blanked for unbiased rating), then compare scores by condition._"

# Blind scoring sheet: answer text with the condition column blanked.
# Use awk to insert truly empty fields (,,) rather than jq @csv which quotes empties as "".
{
  echo "task_id,condition,rep,quality_1to5,answer_text"
  jq -r '[.task_id, .rep, (.answer_text|gsub("[\n,]";" "))] | @csv' "$results" \
    | awk -F',' '{print $1 ",," $2 ",," $3}'
} > scoring.csv
echo
echo "_Blind scoring sheet written to bench/scoring.csv — fill quality_1to5, then re-run with scores._"
