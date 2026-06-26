#!/usr/bin/env bash
# Shared helpers for bench scripts and their tests.
set -euo pipefail

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# assert_eq EXPECTED ACTUAL MESSAGE
assert_eq() {
  if [ "$1" != "$2" ]; then
    printf 'FAIL: %s\n  expected: %q\n  actual:   %q\n' "$3" "$1" "$2" >&2
    return 1
  fi
  printf 'ok: %s\n' "$3"
}

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}
