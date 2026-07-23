package tdd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

// wantHash computes the expected sha256 hex of cmd the same way the production
// code must, so tests never hardcode hex literals.
func wantHash(t *testing.T, cmd string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(cmd))
	return hex.EncodeToString(sum[:])
}

// mustTrustPath resolves the trust path under the (already overridden) HOME.
func mustTrustPath(t *testing.T) string {
	t.Helper()
	p, err := tdd.TrustPath()
	if err != nil {
		t.Fatalf("TrustPath: %v", err)
	}
	return p
}

// @s1: LoadStore on a missing path returns a usable, empty store with no panic.
func TestLoadStore_MissingFile_EmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := mustTrustPath(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("precondition: expected %s to be absent, stat err=%v", path, err)
	}

	store := tdd.LoadStore(path)
	if got := store.HashFor("/anything"); got != "" {
		t.Fatalf("HashFor on empty store = %q, want empty", got)
	}
}

// @s2: LoadStore on malformed JSON returns an empty store, no crash.
func TestLoadStore_MalformedJSON_EmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := mustTrustPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	store := tdd.LoadStore(path)
	if got := store.HashFor("/repo"); got != "" {
		t.Fatalf("HashFor after malformed load = %q, want empty", got)
	}
}

// @s3: SaveTrust then LoadStore round-trips the real sha256 hash of the command.
func TestSaveTrust_RoundTripsCommandHash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const root = "/repo"
	const cmd = "make test"
	want := wantHash(t, cmd)

	if got := tdd.HashCommand(cmd); got != want {
		t.Fatalf("HashCommand(%q) = %q, want %q", cmd, got, want)
	}

	if err := tdd.SaveTrust(root, cmd); err != nil {
		t.Fatalf("SaveTrust: %v", err)
	}

	store := tdd.LoadStore(mustTrustPath(t))
	if got := store.HashFor(root); got != want {
		t.Fatalf("HashFor(%q) = %q, want %q", root, got, want)
	}
}

// @s4: The same command hashes to the same value across repeated saves.
func TestSaveTrust_StableHashAcrossSaves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const root = "/repo"
	const cmd = "make test"
	want := wantHash(t, cmd)

	if err := tdd.SaveTrust(root, cmd); err != nil {
		t.Fatalf("SaveTrust #1: %v", err)
	}
	first := tdd.LoadStore(mustTrustPath(t)).HashFor(root)

	if err := tdd.SaveTrust(root, cmd); err != nil {
		t.Fatalf("SaveTrust #2: %v", err)
	}
	second := tdd.LoadStore(mustTrustPath(t)).HashFor(root)

	if first != second {
		t.Fatalf("hash differs across saves: %q vs %q", first, second)
	}
	if first != want {
		t.Fatalf("stored hash = %q, want real sha256 %q", first, want)
	}
}

// @s5: A symlinked repo path collapses to the canonical (real) key.
func TestSaveTrust_SymlinkCollapsesToRealPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink canonicalization not exercised on Windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	realDir := t.TempDir()
	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	const cmd = "make test"
	want := wantHash(t, cmd)

	if err := tdd.SaveTrust(linkDir, cmd); err != nil {
		t.Fatalf("SaveTrust via symlink: %v", err)
	}

	store := tdd.LoadStore(mustTrustPath(t))
	if got := store.HashFor(realDir); got != want {
		t.Fatalf("HashFor(realPath %q) = %q, want %q", realDir, got, want)
	}
}

// @s6: SaveTrust creates the ~/.tu-agent directory when it is absent.
func TestSaveTrust_CreatesTuAgentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := os.Stat(filepath.Join(home, ".tu-agent")); !os.IsNotExist(err) {
		t.Fatalf("precondition: expected no .tu-agent dir, stat err=%v", err)
	}

	if err := tdd.SaveTrust("/repo", "make test"); err != nil {
		t.Fatalf("SaveTrust: %v", err)
	}

	path := filepath.Join(home, ".tu-agent", "trust.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist after SaveTrust: %v", path, err)
	}
}
