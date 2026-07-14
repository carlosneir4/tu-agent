package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkTempRepo creates a temp dir with a .git marker so repoRoot() anchors on it,
// then chdirs into it (tddCheckCmd.RunE resolves the root via repoRoot()).
func mkTempRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

// writeRole materializes a .claude/agents/<role>.md file under root.
func writeRole(t *testing.T, root, role string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runTddCheck invokes tddCheckCmd.RunE and returns its captured stdout + error.
func runTddCheck(t *testing.T) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	tddCheckCmd.SetOut(&buf)
	t.Cleanup(func() { tddCheckCmd.SetOut(nil) })
	err := tddCheckCmd.RunE(tddCheckCmd, nil)
	return buf.String(), err
}

// @s1 — tdd check succeeds on a repo with an empty .claude dir and no agents
// subdirectory. With the F7-B loadAgentBody fallback every role resolves to an
// embedded generic shell, so a missing .claude/agents is no longer fatal.
// RED today: tddCheckCmd returns an error when any role file is absent.
func TestTddCheckNonFatal_NoAgentsDir(t *testing.T) {
	root := mkTempRepo(t)
	// .claude exists but has no agents subdirectory.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runTddCheck(t)
	if err != nil {
		t.Fatalf("tdd check must succeed without materialized agents, got error: %v", err)
	}
	if !strings.Contains(out, "tdd flow can run") {
		t.Fatalf("tdd check must report the flow can run, want substring %q, got: %q", "tdd flow can run", out)
	}
}

// @s2 — tdd check succeeds when only some roles are materialized. Only
// architect.md is present; the other four role files are absent and must
// resolve via the fallback rather than aborting.
// RED today: the four missing roles make tddCheckCmd return an error.
func TestTddCheckNonFatal_PartialAgents(t *testing.T) {
	root := mkTempRepo(t)
	writeRole(t, root, "architect")

	if _, err := runTddCheck(t); err != nil {
		t.Fatalf("tdd check must succeed with only some roles present, got error: %v", err)
	}
}

// @s3 — tdd check succeeds when every dev-flow role is materialized. Uses the
// deduplicated role set validateTddAgents actually checks (analyst, architect,
// developer, pr-reviewer, scribe). This already passes today via the guard and
// must keep passing after the guard becomes non-fatal.
func TestTddCheckNonFatal_AllAgentsPresent(t *testing.T) {
	root := mkTempRepo(t)
	for _, role := range []string{"analyst", "architect", "developer", "pr-reviewer", "scribe"} {
		writeRole(t, root, role)
	}

	if _, err := runTddCheck(t); err != nil {
		t.Fatalf("tdd check must succeed with all roles present, got error: %v", err)
	}
}

// @s4(a) — the tdd run missing-agents guard is gone. The source of tddRunCmd
// (tdd.go) must no longer contain the "missing dev-flow agents" precondition
// that aborted before dispatch.
// RED today: tdd.go still carries that guard.
func TestTddRunGuardRemoved(t *testing.T) {
	src, err := os.ReadFile("tdd.go")
	if err != nil {
		t.Fatalf("read tdd.go: %v", err)
	}
	if strings.Contains(string(src), "missing dev-flow agents") {
		t.Fatalf("tdd run guard must be removed: tdd.go still contains %q", "missing dev-flow agents")
	}
}
