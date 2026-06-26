#!/usr/bin/env bash
# Tests for the tu-agent shim's non-network resolution logic:
#   1. real tu-agent on PATH wins
#   2. else the auto-installed ~/.tu-agent/bin/tu-agent
#   3. else, with auto-install opted out, a clear 127 (no download)
# The download path needs a real GitHub Release and is verified manually.
set -uo pipefail

SHIM="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/tu-agent"
fails=0
check() { # desc, expected-substring, actual
  if printf '%s' "$3" | grep -qF "$2"; then
    echo "ok   - $1"
  else
    echo "FAIL - $1: expected to contain '$2', got: $3"
    fails=$((fails + 1))
  fi
}

# 1. real tu-agent on PATH is preferred (no download).
t1bin="$(mktemp -d)"
printf '#!/usr/bin/env bash\necho "FAKE-PATH $*"\n' >"$t1bin/tu-agent"
chmod +x "$t1bin/tu-agent"
out="$(HOME="$(mktemp -d)" PATH="$t1bin:/usr/bin:/bin" bash "$SHIM" version 2>&1)"
check "PATH binary is used" "FAKE-PATH version" "$out"

# 2. falls back to the auto-installed binary under \$HOME/.tu-agent/bin.
t2home="$(mktemp -d)"
mkdir -p "$t2home/.tu-agent/bin"
printf '#!/usr/bin/env bash\necho "FAKE-INSTALLED $*"\n' >"$t2home/.tu-agent/bin/tu-agent"
chmod +x "$t2home/.tu-agent/bin/tu-agent"
out="$(HOME="$t2home" PATH="/usr/bin:/bin" bash "$SHIM" mcp 2>&1)"
check "installed binary is used when PATH has none" "FAKE-INSTALLED mcp" "$out"

# 3. opt-out: no binary anywhere, auto-install disabled -> 127, no download.
t3home="$(mktemp -d)"
out="$(HOME="$t3home" PATH="/usr/bin:/bin" TU_AGENT_NO_AUTO_INSTALL=1 bash "$SHIM" version 2>&1)"
code=$?
check "opt-out reports disabled auto-install" "auto-install is disabled" "$out"
if [ "$code" -eq 127 ]; then echo "ok   - opt-out exits 127"; else echo "FAIL - opt-out exit code: $code"; fails=$((fails + 1)); fi

if [ "$fails" -ne 0 ]; then
  echo "FAILED: $fails check(s)"
  exit 1
fi
echo "all shim checks passed"
