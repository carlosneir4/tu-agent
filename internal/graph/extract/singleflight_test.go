//go:build unix

package extract

// Tests for build-single-flight-lock
// (.tu-agent/tdd/graph-robustness-self-heal-single-flight/features/build-single-flight-lock.feature).
//
// Target behavior: BuildScoped takes an exclusive, BLOCKING advisory flock on
// "<root>/.tu-agent/graph.build.lock" before doing any work and releases it
// when done, defensively MkdirAll-ing the .tu-agent dir first.
//
// syscall.Flock is unix-only (darwin + linux), hence the build tag.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// sfInitGitRepo creates a fresh temp dir, git-inits it, and configures a
// throwaway identity so `git add`/enumerateGitFiles work. BuildScoped
// enumerates files via `git ls-files`, so every fixture in this file needs a
// real git repo — a bare t.TempDir() is not enough.
func sfInitGitRepo(t *testing.T) string {
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
	git("config", "user.email", "singleflight@example.com")
	git("config", "user.name", "singleflight")
	return dir
}

// sfWriteGoFile writes a Go source file under dir at rel, creating parents.
func sfWriteGoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sfGitAddAll stages every file in dir so enumerateGitFiles (git ls-files
// --cached --others --exclude-standard) picks it up.
func sfGitAddAll(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "add", "-A")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add -A: %v\n%s", err, out)
	}
}

// sfGoSrc returns a small, distinct, parseable Go source body for index i.
func sfGoSrc(i int) string {
	return fmt.Sprintf("package pkg%d\n\nfunc F%d() int { return %d }\n", i, i, i)
}

// sfLockPath returns the single-flight lock path BuildScoped is expected to
// take: "<root>/.tu-agent/graph.build.lock".
func sfLockPath(root string) string {
	return filepath.Join(root, ".tu-agent", "graph", "graph.build.lock")
}

// @s3 — first build in a repo creates the lock file and succeeds.
func TestBuildScoped_SingleFlight_CreatesLockFile(t *testing.T) {
	dir := sfInitGitRepo(t)
	sfWriteGoFile(t, dir, "pkg/a.go", sfGoSrc(1))
	sfGitAddAll(t, dir)

	if _, err := os.Stat(filepath.Join(dir, ".tu-agent")); err == nil {
		t.Fatalf("fixture setup: .tu-agent must not pre-exist for this scenario")
	}

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	if _, err := BuildScoped(dir, "", Extensions(), st); err != nil {
		t.Fatalf("BuildScoped: %v", err)
	}

	lockPath := sfLockPath(dir)
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("expected lock file %s to exist after BuildScoped, stat err: %v", lockPath, err)
	}
}

// @s2 — a build blocks while the lock is held externally.
//
// Deterministic anchor: BuildScoped must not complete while an external
// process holds an exclusive flock on the lock file — the "has not
// completed" assertion below verifies it stays blocked.
func TestBuildScoped_SingleFlight_BlocksOnExternalLock(t *testing.T) {
	dir := sfInitGitRepo(t)
	sfWriteGoFile(t, dir, "pkg/a.go", sfGoSrc(1))
	sfGitAddAll(t, dir)

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	lockDir := filepath.Dir(sfLockPath(dir))
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", lockDir, err)
	}
	lockFile, err := os.OpenFile(sfLockPath(dir), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock LOCK_EX: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, buildErr := BuildScoped(dir, "", Extensions(), st)
		done <- buildErr
	}()

	select {
	case buildErr := <-done:
		t.Fatalf("BuildScoped completed while the lock was held externally (err=%v); expected it to block", buildErr)
	case <-time.After(400 * time.Millisecond):
		// Expected: still blocked while we hold the external lock.
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatalf("flock LOCK_UN: %v", err)
	}

	select {
	case buildErr := <-done:
		if buildErr != nil {
			t.Errorf("BuildScoped returned error after external lock released: %v", buildErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("BuildScoped did not complete within 10s after the external lock was released")
	}
}

// @s1 — two concurrent builds both complete, store consistent.
//
// Race-regression pin: today BuildScoped never coordinates via the lock file,
// so two goroutines opening separate store.Open handles on the same graph.db
// path race SQLite (store.Open sets MaxOpenConns(1) per connection handle,
// and the busy_timeout is only 5000ms). With enough files the write-heavy
// resolve pass on both sides is likely to overrun the busy timeout and one
// call returns a SQLITE_BUSY-flavored error, OR the two interleaved builds
// leave the file-record count inconsistent with what's on disk. This test's
// primary purpose is to pin the race away once the lock exists; @s2 is the
// deterministic red anchor for the actual locking behavior.
func TestBuildScoped_SingleFlight_ConcurrentBuildsConsistent(t *testing.T) {
	dir := sfInitGitRepo(t)
	const n = 60
	for i := range n {
		sfWriteGoFile(t, dir, fmt.Sprintf("pkg/f%03d.go", i), sfGoSrc(i))
	}
	sfGitAddAll(t, dir)

	dbPath := filepath.Join(dir, "graph.db")
	st1, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open (handle 1): %v", err)
	}
	defer st1.Close()
	st2, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open (handle 2): %v", err)
	}
	defer st2.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = BuildScoped(dir, "", Extensions(), st1)
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = BuildScoped(dir, "", Extensions(), st2)
	}()
	wg.Wait()

	for i, buildErr := range errs {
		if buildErr != nil {
			t.Errorf("concurrent BuildScoped[%d] returned error (expected nil under a single-flight lock): %v", i, buildErr)
		}
	}

	files, err := st1.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != n {
		t.Errorf("store has %d file records, want %d (on-disk file count)", len(files), n)
	}
	for i := range n {
		rel := fmt.Sprintf("pkg/f%03d.go", i)
		if _, ok := files[rel]; !ok {
			t.Errorf("missing file record for %s after concurrent builds", rel)
		}
	}
}
