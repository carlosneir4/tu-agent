package tdd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs a git command in dir, failing the test on error, and returns
// the trimmed combined output. Fixtures are real git repositories built in
// t.TempDir(), mirroring the worktree_test.go convention (no mocks).
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// @s1 — Merge-base and changed files resolve on a normal feature branch.
func TestReviewScopeNormalBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	gitInit(t, root)

	// Default branch `main` with an initial commit.
	writeFile(t, root, "seed.txt", "seed")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "seed")
	runGit(t, root, "branch", "-M", "main")

	// Feature branch diverges from main and adds a committed change.
	runGit(t, root, "checkout", "-q", "-b", "feature")
	writeFile(t, root, "feature.txt", "feature work")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "feature change")

	// A later commit on main so main and feature genuinely diverge and the
	// merge-base is an ancestor of both, not either tip.
	runGit(t, root, "checkout", "-q", "main")
	writeFile(t, root, "main-only.txt", "later main work")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "main change")
	runGit(t, root, "checkout", "-q", "feature")

	wantBase := runGit(t, root, "merge-base", "main", "feature")

	base, files, skip, err := ReviewScope(context.Background(), root)
	if err != nil {
		t.Fatalf("ReviewScope error = %v, want nil", err)
	}
	if skip != "" {
		t.Fatalf("skip reason = %q, want empty", skip)
	}
	if base != wantBase {
		t.Fatalf("base = %q, want merge-base %q", base, wantBase)
	}
	if len(files) == 0 {
		t.Fatalf("changed files = %v, want non-empty", files)
	}
	if !containsPath(files, "feature.txt") {
		t.Fatalf("changed files = %v, want to include feature.txt", files)
	}
	if containsPath(files, "main-only.txt") {
		t.Fatalf("changed files = %v, must not include main-only.txt (not on the branch)", files)
	}
}

// @s2 — Unresolvable merge-base (no detectable default branch) yields a skip
// reason, not an error.
func TestReviewScopeNoDefaultBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	gitInit(t, root)

	// The repository's only branch is the feature branch itself: no origin
	// remote, no main, no master, so the default branch is undetectable.
	writeFile(t, root, "seed.txt", "seed")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "seed")
	runGit(t, root, "branch", "-M", "feature")

	base, files, skip, err := ReviewScope(context.Background(), root)
	if err != nil {
		t.Fatalf("ReviewScope error = %v, want nil", err)
	}
	if skip == "" {
		t.Fatalf("skip reason is empty, want a reason naming the merge-base failure")
	}
	if !strings.Contains(strings.ToLower(skip), "merge-base") {
		t.Fatalf("skip reason = %q, want it to name the merge-base failure", skip)
	}
	if base != "" {
		t.Fatalf("base = %q, want empty when the merge-base cannot be resolved", base)
	}
	if len(files) != 0 {
		t.Fatalf("changed files = %v, want none when scope is skipped", files)
	}
}

// @s3 — Empty branch diff (no changes since the merge-base) yields a skip
// reason.
func TestReviewScopeEmptyDiff(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	gitInit(t, root)

	// Default branch `main` with a commit; feature branch points at the same
	// commit, so HEAD equals the merge-base and the branch diff is empty.
	writeFile(t, root, "seed.txt", "seed")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "seed")
	runGit(t, root, "branch", "-M", "main")
	runGit(t, root, "checkout", "-q", "-b", "feature")

	_, files, skip, err := ReviewScope(context.Background(), root)
	if err != nil {
		t.Fatalf("ReviewScope error = %v, want nil", err)
	}
	if skip == "" {
		t.Fatalf("skip reason is empty, want a reason stating the diff is empty")
	}
	if !strings.Contains(strings.ToLower(skip), "empty") {
		t.Fatalf("skip reason = %q, want it to state the diff is empty", skip)
	}
	if len(files) != 0 {
		t.Fatalf("changed files = %v, want none for an empty diff", files)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if strings.ReplaceAll(p, "\\", "/") == want {
			return true
		}
	}
	return false
}
