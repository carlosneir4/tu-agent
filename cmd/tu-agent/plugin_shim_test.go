package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pluginShimPath resolves plugin/bin/tu-agent relative to this package,
// mirroring how loadPluginHooks (plugin_hooks_test.go) locates
// plugin/hooks/hooks.json.
func pluginShimPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "plugin", "bin", "tu-agent")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("locating %s: %v", path, err)
	}
	return path
}

// TestPluginShimSyntax (@s1): the shim must remain valid bash after the
// checksum-required fix lands. This is a guard, not the red target for this
// stage — it passes today and must keep passing.
func TestPluginShimSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	path := pluginShimPath(t)
	cmd := exec.Command("bash", "-n", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n %s failed: %v\n%s", path, err, out)
	}
}

// installBinaryBody extracts the source text of the install_binary()
// function from plugin/bin/tu-agent: from its "install_binary() {" line to
// the matching closing brace on its own line. Functions in this script are
// not nested and every closing brace sits at column 0, so a simple
// line-by-line scan for a bare "}" is a robust-enough slice (same style as
// TestPluginHooksConfig's text-based parsing of hooks.json).
func installBinaryBody(t *testing.T) string {
	t.Helper()
	path := pluginShimPath(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	text := string(raw)
	start := strings.Index(text, "install_binary()")
	if start == -1 {
		t.Fatal("install_binary() function not found in plugin/bin/tu-agent")
	}
	lines := strings.Split(text[start:], "\n")
	end := -1
	for i, line := range lines {
		if i == 0 {
			continue // the "install_binary() {" line itself
		}
		if line == "}" {
			end = i
			break
		}
	}
	if end == -1 {
		t.Fatal("could not find closing brace for install_binary()")
	}
	return strings.Join(lines[:end+1], "\n")
}

// hasEmptyWantAbort reports whether the given text rejects an empty `want`
// (the checksum computed by remote_sha) with a non-zero return, in either of
// two equivalent shapes:
//   - an explicit `[ -z "$want" ]` (or `[[ -z "$want" ]]`) guard followed by
//     `return 1` shortly after, or
//   - the existing `if [ -n "$want" ]; then ... fi` guard grown an `else`
//     branch that itself contains `return 1` for the empty case.
func hasEmptyWantAbort(text string) bool {
	nearbyReturn1 := func(tail string) bool {
		window := tail
		if len(window) > 400 {
			window = window[:400]
		}
		return strings.Contains(window, "return 1")
	}

	explicit := regexp.MustCompile(`\[\[?\s*-z\s*"\$want"\s*\]\]?`)
	if loc := explicit.FindStringIndex(text); loc != nil && nearbyReturn1(text[loc[1]:]) {
		return true
	}

	posGuard := regexp.MustCompile(`if \[ -n "\$want" \]; then`)
	if loc := posGuard.FindStringIndex(text); loc != nil {
		tail := text[loc[1]:]
		if len(tail) > 400 {
			tail = tail[:400]
		}
		if elseIdx := strings.Index(tail, "else"); elseIdx != -1 {
			if strings.Contains(tail[elseIdx:], "return 1") {
				return true
			}
		}
	}
	return false
}

// hasManualInstallStderrMessage reports whether the text writes a message to
// stderr (">&2") that mentions manual installation, within a reasonable
// window of the stderr redirect.
func hasManualInstallStderrMessage(text string) bool {
	stderrRe := regexp.MustCompile(`>&2`)
	manualRe := regexp.MustCompile(`(?i)manual`)
	for _, loc := range stderrRe.FindAllStringIndex(text, -1) {
		lo, hi := loc[0]-300, loc[1]+300
		if lo < 0 {
			lo = 0
		}
		if hi > len(text) {
			hi = len(text)
		}
		if manualRe.MatchString(text[lo:hi]) {
			return true
		}
	}
	return false
}

// TestPluginShimInstallAbortsOnEmptyChecksum (@s2, @s3): pins the fix
// contract for install_binary in plugin/bin/tu-agent. Today install_binary
// computes `want` via remote_sha and only compares checksums
// `if [ -n "$want" ]`; when SHA256SUMS is unreachable, `want` is empty, the
// compare is skipped entirely, and the function falls through past the
// tar-extract/mv steps and installs the downloaded binary UNVERIFIED. The
// fix (next stage) must abort (return 1) before installing when `want` is
// empty, with a stderr message pointing at manual installation. This test is
// RED until that guard exists.
func TestPluginShimInstallAbortsOnEmptyChecksum(t *testing.T) {
	body := installBinaryBody(t)

	wantIdx := strings.Index(body, `want="$(remote_sha`)
	if wantIdx == -1 {
		t.Fatal("install_binary no longer computes `want` via remote_sha — test assumptions stale")
	}

	mvRe := regexp.MustCompile(`\bmv\s+"\$\{tmp\}/tu-agent"\s+"\$INSTALL_BIN"`)
	mvLoc := mvRe.FindStringIndex(body)
	if mvLoc == nil {
		t.Fatal("install_binary no longer moves the binary into $INSTALL_BIN — test assumptions stale")
	}
	if mvLoc[0] <= wantIdx {
		t.Fatal("mv into $INSTALL_BIN appears before the want computation — test assumptions stale")
	}

	// The guard region is everything between computing `want` and installing
	// the binary: this is where the empty-checksum abort must live.
	guardRegion := body[wantIdx:mvLoc[0]]

	if !hasEmptyWantAbort(guardRegion) {
		t.Errorf(
			"install_binary has no empty-`want` abort (return 1) before the mv into $INSTALL_BIN; "+
				"an unreachable SHA256SUMS falls through to an unverified install. guard region:\n%s",
			guardRegion,
		)
	}

	if !hasManualInstallStderrMessage(guardRegion) {
		t.Errorf(
			"install_binary's empty-`want` path has no stderr (>&2) message pointing at manual "+
				"installation. guard region:\n%s",
			guardRegion,
		)
	}

	// Inverse/regression guard: the historical vulnerability is that
	// "if [ -n "$want" ]" guards the checksum COMPARE only, with no paired
	// rejection for the empty case, so an empty want silently skips
	// verification rather than aborting. Pin the paired rejection directly —
	// not merely that the old guard string is gone, since it may legitimately
	// remain as the guard around the sha comparison itself.
	if strings.Contains(guardRegion, `if [ -n "$want" ]`) && !hasEmptyWantAbort(guardRegion) {
		t.Error(
			`install_binary still guards the checksum compare with "if [ -n \"$want\" ]" but has no ` +
				"paired empty-want rejection: an unreachable SHA256SUMS falls through to an unverified install",
		)
	}
}
