#!/usr/bin/env bash
# Run the cold/graph/learned matrix against a repo and print the 3-way report.
# Always restores the repo's knowledge artifacts (trap), even on failure.
# Usage:
#   compare-knowledge.sh --repo <path> --tasks <tasks.jsonl> --model <m> [--reps N] [--max-turns N]
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh

CONDITIONS=(cold graph learned)

run_matrix() { # repo tasks model reps maxturns
  local repo="$1" tasks="$2" model="$3" reps="$4" maxt="$5"
  : > results.jsonl   # fresh results for this matrix
  trap 'ROOT="$repo" ./with-learn.sh restore >/dev/null 2>&1 || true' EXIT
  local c
  for c in "${CONDITIONS[@]}"; do
    echo "== condition: $c =="
    ROOT="$repo" ./with-learn.sh "$c"
    ./run.sh --condition "$c" --repo "$repo" --tasks "$tasks" --model "$model" --reps "$reps" --max-turns "$maxt"
  done
  ROOT="$repo" ./with-learn.sh restore
  trap - EXIT
  ./report.sh results.jsonl
}

main() {
  local repo="" tasks="" model="" reps=2 maxt=30
  while [ $# -gt 0 ]; do case "$1" in
    --repo) repo="$2"; shift 2;;
    --tasks) tasks="$2"; shift 2;;
    --model) model="$2"; shift 2;;
    --reps) reps="$2"; shift 2;;
    --max-turns) maxt="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac; done
  [ -n "$repo" ]  || { echo "--repo required" >&2; exit 2; }
  [ -n "$tasks" ] || { echo "--tasks required" >&2; exit 2; }
  [ -n "$model" ] || { echo "--model required" >&2; exit 2; }
  require jq; require claude
  run_matrix "$repo" "$tasks" "$model" "$reps" "$maxt"
}

if [ "${1:-}" = "--source-only" ]; then return 0 2>/dev/null || exit 0; fi
main "$@"
