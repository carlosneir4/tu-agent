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
#    (no-auto-update so the check stays hermetic regardless of network/releases)
t2home="$(mktemp -d)"
mkdir -p "$t2home/.tu-agent/bin"
printf '#!/usr/bin/env bash\necho "FAKE-INSTALLED $*"\n' >"$t2home/.tu-agent/bin/tu-agent"
chmod +x "$t2home/.tu-agent/bin/tu-agent"
out="$(HOME="$t2home" PATH="/usr/bin:/bin" TU_AGENT_NO_AUTO_UPDATE=1 bash "$SHIM" mcp 2>&1)"
check "installed binary is used when PATH has none" "FAKE-INSTALLED mcp" "$out"

# 3. opt-out: no binary anywhere, auto-install disabled -> 127, no download.
t3home="$(mktemp -d)"
out="$(HOME="$t3home" PATH="/usr/bin:/bin" TU_AGENT_NO_AUTO_INSTALL=1 bash "$SHIM" version 2>&1)"
code=$?
check "opt-out reports disabled auto-install" "auto-install is disabled" "$out"
if [ "$code" -eq 127 ]; then echo "ok   - opt-out exits 127"; else echo "FAIL - opt-out exit code: $code"; fails=$((fails + 1)); fi

# --- auto-update (offline, via a fake file:// release base) ---------------------
# asset name + checksum tool for this platform (mirrors the shim).
case "$(uname -s)" in Darwin) _goos=darwin ;; Linux) _goos=linux ;; *) _goos=unsupported ;; esac
case "$(uname -m)" in arm64 | aarch64) _goarch=arm64 ;; x86_64 | amd64) _goarch=amd64 ;; *) _goarch=unsupported ;; esac
ASSET="tu-agent-${_goos}-${_goarch}.tar.gz"
if command -v sha256sum >/dev/null 2>&1; then _SHA() { sha256sum "$1" | awk '{print $1}'; }; else _SHA() { shasum -a 256 "$1" | awk '{print $1}'; }; fi

make_release() { # dir, marker — builds a fake release dir with the asset + SHA256SUMS
  local d="$1" m="$2"
  mkdir -p "$d"
  printf '#!/usr/bin/env bash\necho "%s $*"\n' "$m" >"$d/tu-agent"
  chmod +x "$d/tu-agent"
  tar -C "$d" -czf "$d/$ASSET" tu-agent
  (cd "$d" && _SHA "$ASSET" | awk -v a="$ASSET" '{print $1"  "a}' >SHA256SUMS)
  rm -f "$d/tu-agent"
}

if [ "$_goos" != "unsupported" ] && [ "$_goarch" != "unsupported" ]; then
  # 4. opt-out: a stale stamp + a newer release, but TU_AGENT_NO_AUTO_UPDATE=1 -> no refresh.
  uh="$(mktemp -d)"; mkdir -p "$uh/.tu-agent/bin"
  printf '#!/usr/bin/env bash\necho "OLD $*"\n' >"$uh/.tu-agent/bin/tu-agent"; chmod +x "$uh/.tu-agent/bin/tu-agent"
  printf '0 deadbeef\n' >"$uh/.tu-agent/bin/.update-stamp"
  ur="$(mktemp -d)"; make_release "$ur" "NEW"
  out="$(HOME="$uh" PATH="/usr/bin:/bin" TU_AGENT_NO_AUTO_UPDATE=1 TU_AGENT_RELEASE_BASE="file://$ur" bash "$SHIM" version 2>&1)"
  check "opt-out skips auto-update" "OLD version" "$out"

  # 5. throttle: fresh stamp + a newer release -> skipped (not yet due).
  uh="$(mktemp -d)"; mkdir -p "$uh/.tu-agent/bin"
  printf '#!/usr/bin/env bash\necho "OLD $*"\n' >"$uh/.tu-agent/bin/tu-agent"; chmod +x "$uh/.tu-agent/bin/tu-agent"
  printf '%s deadbeef\n' "$(date +%s)" >"$uh/.tu-agent/bin/.update-stamp"
  ur="$(mktemp -d)"; make_release "$ur" "NEW"
  out="$(HOME="$uh" PATH="/usr/bin:/bin" TU_AGENT_RELEASE_BASE="file://$ur" bash "$SHIM" version 2>&1)"
  check "fresh stamp throttles the update" "OLD version" "$out"

  # 6. due + different sha -> refreshes to the new binary.
  uh="$(mktemp -d)"; mkdir -p "$uh/.tu-agent/bin"
  printf '#!/usr/bin/env bash\necho "OLD $*"\n' >"$uh/.tu-agent/bin/tu-agent"; chmod +x "$uh/.tu-agent/bin/tu-agent"
  printf '0 deadbeef\n' >"$uh/.tu-agent/bin/.update-stamp"
  ur="$(mktemp -d)"; make_release "$ur" "NEW"
  out="$(HOME="$uh" PATH="/usr/bin:/bin" TU_AGENT_RELEASE_BASE="file://$ur" bash "$SHIM" version 2>&1)"
  check "due check with new sha refreshes the binary" "NEW version" "$out"

  # 7. due but sha already matches -> no re-download.
  uh="$(mktemp -d)"; mkdir -p "$uh/.tu-agent/bin"
  printf '#!/usr/bin/env bash\necho "OLD $*"\n' >"$uh/.tu-agent/bin/tu-agent"; chmod +x "$uh/.tu-agent/bin/tu-agent"
  ur="$(mktemp -d)"; make_release "$ur" "NEW"
  cursha="$(awk '{print $1}' "$ur/SHA256SUMS")"
  printf '0 %s\n' "$cursha" >"$uh/.tu-agent/bin/.update-stamp"
  out="$(HOME="$uh" PATH="/usr/bin:/bin" TU_AGENT_RELEASE_BASE="file://$ur" bash "$SHIM" version 2>&1)"
  check "matching sha skips re-download" "OLD version" "$out"
fi

if [ "$fails" -ne 0 ]; then
  echo "FAILED: $fails check(s)"
  exit 1
fi
echo "all shim checks passed"
