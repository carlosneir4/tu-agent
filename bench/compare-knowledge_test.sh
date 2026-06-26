#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh

# Source-only must expose the condition list and not invoke claude.
source ./compare-knowledge.sh --source-only
assert_eq "cold graph learned" "${CONDITIONS[*]}" "compare-knowledge defines the 3 conditions in order"
assert_eq "function" "$(type -t run_matrix)" "run_matrix is a function"
