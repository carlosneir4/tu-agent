package extract

// Tests for build-skip-missing-file
// (.tu-agent/tdd/graph-robustness-self-heal-single-flight/features/build-skip-missing-file.feature).
//
// Target behavior: an os.ReadFile or os.Stat failure inside BuildScoped's
// parse loop (build.go:112-118) must SKIP the file with a slog warning (the
// same pattern as the parse-failed skip at build.go:131-139) instead of
// aborting the whole build — not counted as Parsed, not counted as Failed, no
// "failed" row for it in the store, and the build continues with the
// remaining files.
//
// Deterministic injection: the feature file's suggested "delete a committed
// file from the worktree" does NOT reach the parse loop — enumerateGitFiles
// (build.go:369) already os.Stat's every candidate during enumeration and
// silently drops missing paths (see TestBuildGit_DeletedUnstaged), so a
// deleted file never gets past step 1 and such a test would pass vacuously
// today. Instead: commit a parseable .go file, then replace it with a
// DIRECTORY of the same name. git ls-files still enumerates it (it is in the
// index), enumeration's os.Stat succeeds (directories stat fine), and
// os.ReadFile then fails with EISDIR inside the parse loop — exactly the
// mid-build-vanish window this feature targets.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// smInitGitRepo creates a fresh temp dir, git-inits it, and configures a
// throwaway identity so `git add`/`git commit`/enumerateGitFiles work.
// Uniquely named (sm = skip-missing) to avoid colliding with sfInitGitRepo in
// singleflight_test.go, which is unix-build-tagged and unavailable here.
func smInitGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "skipmissing@example.com")
	git("config", "user.name", "skipmissing")
	return dir
}

// smSetupVanishedFileFixture creates a git repo with two committed,
// parseable .go files ("keep.go" and "vanish.go"), then replaces vanish.go
// with a directory of the same name to simulate the file vanishing between
// enumeration and parse. Returns the repo root.
func smSetupVanishedFileFixture(t *testing.T) string {
	t.Helper()
	dir := smInitGitRepo(t)

	writeFixture(t, dir, "keep.go", "package keep\n\nfunc Keep() {}\n")
	writeFixture(t, dir, "vanish.go", "package vanish\n\nfunc Vanish() {}\n")

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("add", "-A")
	git("commit", "-m", "initial commit")

	// Replace the committed file with a directory of the same name: still
	// enumerated (it is in the git index) and still passes enumeration's own
	// os.Stat (directories stat fine), but os.ReadFile in the parse loop
	// fails with EISDIR. Keep the ".go" suffix on the directory name so it
	// still passes the extension filter.
	vanishPath := filepath.Join(dir, "vanish.go")
	if err := os.Remove(vanishPath); err != nil {
		t.Fatalf("remove vanish.go: %v", err)
	}
	if err := os.Mkdir(vanishPath, 0o755); err != nil {
		t.Fatalf("mkdir vanish.go (as directory): %v", err)
	}
	return dir
}

// @s1: a vanished file does not abort the build — the surviving file is
// parsed and present in the store.
func TestBuildScoped_SkipMissingFile_DoesNotAbort(t *testing.T) {
	dir := smSetupVanishedFileFixture(t)

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".go"}, st); err != nil {
		t.Fatalf("Build returned error for a file that vanished mid-build (replaced by a directory): %v", err)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	rec, ok := files["keep.go"]
	if !ok {
		t.Fatal("keep.go should be present in the store after Build")
	}
	if rec.Status != "ok" {
		t.Errorf("keep.go status = %q, want %q", rec.Status, "ok")
	}

	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	var foundKeep bool
	for _, n := range nodes {
		if strings.HasPrefix(n.ID, "keep.go") {
			foundKeep = true
			break
		}
	}
	if !foundKeep {
		t.Errorf("expected nodes from keep.go, got none; all nodes: %+v", nodes)
	}
}

// @s2: the vanished file is neither Parsed nor marked failed.
func TestBuildScoped_SkipMissingFile_NotCountedFailed(t *testing.T) {
	dir := smSetupVanishedFileFixture(t)

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	result, err := Build(dir, []string{".go"}, st)
	if err != nil {
		t.Fatalf("Build returned error for a file that vanished mid-build (replaced by a directory): %v", err)
	}

	if result.Parsed != 1 {
		t.Errorf("result.Parsed = %d, want 1 (only keep.go)", result.Parsed)
	}
	if result.Failed != 0 {
		t.Errorf("result.Failed = %d, want 0 (a vanished file is skipped, not a parse failure)", result.Failed)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if rec, ok := files["vanish.go"]; ok && rec.Status == "failed" {
		t.Errorf(`vanish.go must not have a status="failed" row in the store, got %+v`, rec)
	}
}
