package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustTouch creates an empty file at path, MkdirAll'ing its parent. It fails the
// test on any error so a broken fixture is never mistaken for a real assertion.
func mustTouch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent of %q: %v", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %q: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %q: %v", path, err)
	}
}

// TestOldLayoutGuard pins the loud old-layout guard (old-layout-guard feature,
// scenarios @s1 @s2 @s3 plus the chunks-prefix trap). Each case builds a repo
// fixture under t.TempDir() and asserts oldLayoutGuard's return and — for @s1 —
// that the guard created nothing. These are RED against the always-nil stub in
// oldlayout.go and turn GREEN when the real detection lands.
func TestOldLayoutGuard(t *testing.T) {
	t.Run("@s1 old flat markers, new absent -> violation naming each move", func(t *testing.T) {
		root := t.TempDir()
		mustTouch(t, filepath.Join(root, ".tu-agent", "memory.db"))
		mustTouch(t, filepath.Join(root, ".tu-agent", "rules.md"))
		// Deliberately create NONE of the new-layout paths.

		err := oldLayoutGuard(root)
		if err == nil {
			t.Fatalf("oldLayoutGuard returned nil; want a non-nil error naming old paths to move")
		}
		msg := err.Error()
		wantSubstrings := []string{
			".tu-agent/memory.db -> .tu-agent/memory/memory.db",
			".tu-agent/rules.md -> .tu-agent/rules/all.md",
		}
		for _, want := range wantSubstrings {
			if !strings.Contains(msg, want) {
				t.Errorf("error message %q does not contain %q", msg, want)
			}
		}

		// The guard is pure detection: it must not create the new memory.db.
		if _, statErr := os.Stat(memoryDBPath(root)); !os.IsNotExist(statErr) {
			t.Errorf("guard created %q (stat err = %v); it must create nothing", memoryDBPath(root), statErr)
		}
	})

	t.Run("@s2 new memory.db present wins over leftover old memory.db -> nil", func(t *testing.T) {
		root := t.TempDir()
		// New path present (create its dir + empty file).
		mustTouch(t, memoryDBPath(root))
		// Leftover old flat marker still on disk.
		mustTouch(t, filepath.Join(root, ".tu-agent", "memory.db"))

		if err := oldLayoutGuard(root); err != nil {
			t.Errorf("oldLayoutGuard returned %v; want nil (new layout wins over old leftover)", err)
		}
	})

	t.Run("@s3 no .tu-agent dir at all -> nil", func(t *testing.T) {
		root := t.TempDir()
		// No .tu-agent directory created.

		if err := oldLayoutGuard(root); err != nil {
			t.Errorf("oldLayoutGuard returned %v; want nil (fresh repo)", err)
		}
	})

	t.Run("chunks-prefix trap: new memory subsystem dir alone -> nil", func(t *testing.T) {
		root := t.TempDir()
		// Only the NEW memory subsystem dir exists (via its new memory.db), and
		// there are NO old chunk files and no old flat markers. The shared
		// "memory/" prefix must not be read as the old chunks dir.
		mustTouch(t, memoryDBPath(root))

		if err := oldLayoutGuard(root); err != nil {
			t.Errorf("oldLayoutGuard returned %v; want nil (memory/ prefix is not the old chunks signal)", err)
		}
	})

	t.Run("old chunk file present, new chunks dir absent -> violation naming chunk move", func(t *testing.T) {
		root := t.TempDir()
		// An actual old chunk FILE under the old chunks glob location.
		mustTouch(t, filepath.Join(root, ".tu-agent", "memory", "chunks", "chunk-alice.jsonl.gz"))
		// New share/memory/chunks deliberately absent.

		err := oldLayoutGuard(root)
		if err == nil {
			t.Fatalf("oldLayoutGuard returned nil; want a violation for the old chunk file")
		}
		want := ".tu-agent/memory/chunks -> .tu-agent/share/memory/chunks"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message %q does not contain %q", err.Error(), want)
		}
	})
}
