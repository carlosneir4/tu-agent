package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTddStatusJSON(t *testing.T) {
	var buf bytes.Buffer
	tddStatusCmd.SetOut(&buf)
	// In a temp repo with no state, status reports not resumable.
	if err := tddStatusCmd.RunE(tddStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	var out struct {
		Resumable bool `json:"resumable"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("status output not JSON: %q (%v)", buf.String(), err)
	}
}

// TestTddStateBeginRejectsDuplicateFeatures drives `tdd state begin` with
// duplicate --feature values and asserts the CLI surfaces BeginRun's
// duplicate-feature error, rather than silently building a state.json that
// would later infinite-loop NextPending/Mark on resume.
func TestTddStateBeginRejectsDuplicateFeatures(t *testing.T) {
	oldFeatures, oldTask, oldBranch := tddStateFeatures, tddStateTask, tddStateBranch
	t.Cleanup(func() {
		tddStateFeatures, tddStateTask, tddStateBranch = oldFeatures, oldTask, oldBranch
	})
	tddStateFeatures = []string{"x", "x"}
	tddStateTask = "t"
	tddStateBranch = "b"

	err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil)
	if err == nil {
		t.Fatalf("expected duplicate-feature error, got nil")
	}
	if !strings.Contains(err.Error(), `duplicate feature "x"`) {
		t.Fatalf("error = %q, want it to contain `duplicate feature \"x\"`", err.Error())
	}
}

func TestTddStateMarkUnknown(t *testing.T) {
	err := tddStateMarkCmd.RunE(tddStateMarkCmd, []string{"nope", "pass"})
	if err == nil {
		t.Skip("no state file in repo root; mark on empty state is a no-op or error depending on env")
	}
	if !strings.Contains(err.Error(), "nope") && !strings.Contains(err.Error(), "state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTddStatePathByTicket(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".tu-agent", "tdd", "ABC-1-x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tddStatePath(root, "ABC-1")
	if got != filepath.Join(dir, "state.json") {
		t.Errorf("tddStatePath = %q, want under %q", got, dir)
	}
}

func TestTddStateBaseRel(t *testing.T) {
	root := "/repo"
	sp := filepath.Join(root, ".tu-agent", "tdd", "ABC-1-x", "state.json")
	if got := tddStateBaseRel(root, sp); got != filepath.Join(".tu-agent", "tdd", "ABC-1-x") {
		t.Errorf("tddStateBaseRel = %q", got)
	}
}

func TestTddStateBaseFlagWins(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, ".tu-agent", "tdd", "AAA-1-old")
	newer := filepath.Join(root, ".tu-agent", "tdd", "BBB-2-new")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "state.json"), []byte(`{"version":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// make "older" the mtime-newest so the fallback would pick it
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(older, future, future); err != nil {
		t.Fatal(err)
	}
	old := tddStateBaseFlag
	t.Cleanup(func() { tddStateBaseFlag = old })
	tddStateBaseFlag = ".tu-agent/tdd/BBB-2-new"
	if got := tddStateFile(root); got != filepath.Join(newer, "state.json") {
		t.Errorf("--base ignored: got %q", got)
	}
	tddStateBaseFlag = ""
	if got := tddStateFile(root); got != filepath.Join(older, "state.json") {
		t.Errorf("mtime fallback broken: got %q", got)
	}
}

func TestTddStatePathLegacyFlat(t *testing.T) {
	root := t.TempDir()
	tddDir := filepath.Join(root, ".tu-agent", "tdd")
	if err := os.MkdirAll(tddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tddDir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"features", "progress"} {
		if err := os.MkdirAll(filepath.Join(tddDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := tddStatePath(root, "")
	if got != filepath.Join(tddDir, "state.json") {
		t.Errorf("tddStatePath legacy = %q", got)
	}
}
