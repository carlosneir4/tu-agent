#!/usr/bin/env bash
# Runs every bench/*_test.sh and reports a summary.
set -euo pipefail
cd "$(dirname "$0")"
fail=0
for t in *_test.sh; do
  [ -e "$t" ] || { echo "no tests yet"; exit 0; }
  echo "== $t =="
  if bash "$t"; then :; else fail=1; fi
done
[ "$fail" -eq 0 ] && echo "ALL BENCH TESTS PASSED" || { echo "BENCH TESTS FAILED"; exit 1; }
