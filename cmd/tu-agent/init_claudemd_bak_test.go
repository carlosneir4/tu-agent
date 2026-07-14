package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// Feature: init-force-claudemd-bak (@f0-4)
//
// generateClaudeMD (cmd/tu-agent/init.go:160) must back up an existing
// CLAUDE.md to "CLAUDE.md.bak" BEFORE overwriting, but only when the
// overwrite will actually happen (file exists AND force=true) — mirroring
// the agents' backup pattern at init.go:214-218. A failed backup write is a
// hard failure: generateClaudeMD returns an error and CLAUDE.md itself is
// not overwritten.

// TestGenerateClaudeMD_ForceBacksUpExisting (@s1): --force over an existing
// CLAUDE.md creates a .bak with the old content, and CLAUDE.md ends up
// holding the freshly rendered template.
func TestGenerateClaudeMD_ForceBacksUpExisting(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	const sentinel = "SENTINEL: hand-edited CLAUDE.md, do not lose me"
	if err := os.WriteFile("CLAUDE.md", []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	if err := generateClaudeMD(info, "go", "go build ./...", "go test ./...", true); err != nil {
		t.Fatalf("generateClaudeMD: %v", err)
	}

	bak, err := os.ReadFile("CLAUDE.md.bak")
	if err != nil {
		t.Fatalf("expected CLAUDE.md.bak to exist: %v", err)
	}
	if string(bak) != sentinel {
		t.Errorf("CLAUDE.md.bak = %q, want exact pre-overwrite content %q", bak, sentinel)
	}

	regenerated, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatalf("expected CLAUDE.md to exist: %v", err)
	}
	if string(regenerated) == sentinel {
		t.Error("CLAUDE.md should have been regenerated from the template, not left as the sentinel content")
	}
	if !strings.Contains(string(regenerated), "example-project") {
		t.Errorf("CLAUDE.md does not look like the freshly rendered template (missing project name): %q", regenerated)
	}
	if !strings.Contains(string(regenerated), "go test ./...") {
		t.Errorf("CLAUDE.md does not look like the freshly rendered template (missing test command): %q", regenerated)
	}
}

// TestGenerateClaudeMD_NoForceLeavesFileAndSkipsBackup (@s2): without
// --force, an existing CLAUDE.md is left unchanged and no new
// CLAUDE.md.bak is written.
func TestGenerateClaudeMD_NoForceLeavesFileAndSkipsBackup(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	const existing = "hand-edited CLAUDE.md, force=false must not touch this"
	if err := os.WriteFile("CLAUDE.md", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	if err := generateClaudeMD(info, "go", "go build ./...", "go test ./...", false); err != nil {
		t.Fatalf("generateClaudeMD: %v", err)
	}

	unchanged, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatalf("expected CLAUDE.md to exist: %v", err)
	}
	if string(unchanged) != existing {
		t.Errorf("CLAUDE.md = %q, want unchanged %q (force=false must skip the write)", unchanged, existing)
	}

	if _, err := os.Stat("CLAUDE.md.bak"); err == nil {
		t.Error("CLAUDE.md.bak should not have been created when force=false")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error statting CLAUDE.md.bak: %v", err)
	}
}

// TestGenerateClaudeMD_RepeatForceKeepsOriginalBackup (PR-review regression):
// a second --force run must NOT clobber CLAUDE.md.bak — which holds the
// hand-edited original — with the generated content the first run wrote to
// CLAUDE.md. The backup is write-once (writeBackupOnce, O_EXCL).
func TestGenerateClaudeMD_RepeatForceKeepsOriginalBackup(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	const sentinel = "SENTINEL: hand-edited CLAUDE.md, do not lose me"
	if err := os.WriteFile("CLAUDE.md", []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	for run := 1; run <= 2; run++ {
		if err := generateClaudeMD(info, "go", "go build ./...", "go test ./...", true); err != nil {
			t.Fatalf("generateClaudeMD run %d: %v", run, err)
		}
	}

	bak, err := os.ReadFile("CLAUDE.md.bak")
	if err != nil {
		t.Fatalf("expected CLAUDE.md.bak to exist: %v", err)
	}
	if string(bak) != sentinel {
		t.Errorf("CLAUDE.md.bak = %q after a repeat --force, want the original %q (write-once backup)", bak, sentinel)
	}
}

// TestGenerateClaudeMD_BackupFailureAbortsOverwrite (@s3): if the backup
// write itself cannot succeed (simulated here by making CLAUDE.md.bak an
// existing directory, an OS-portable and UID-independent failure mode),
// generateClaudeMD must return an error and must not overwrite the live
// CLAUDE.md.
func TestGenerateClaudeMD_BackupFailureAbortsOverwrite(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	const sentinel = "SENTINEL: hand-edited CLAUDE.md, do not lose me"
	if err := os.WriteFile("CLAUDE.md", []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the backup destination an existing, non-empty directory so any
	// os.WriteFile/os.Rename onto it fails regardless of platform or UID
	// (root-run CI ignores permission bits, but this fails unconditionally).
	if err := os.MkdirAll(filepath.Join("CLAUDE.md.bak", "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	genErr := generateClaudeMD(info, "go", "go build ./...", "go test ./...", true)
	if genErr == nil {
		t.Fatal("expected generateClaudeMD to return an error when the backup write fails")
	}

	unchanged, readErr := os.ReadFile("CLAUDE.md")
	if readErr != nil {
		t.Fatalf("expected CLAUDE.md to still exist: %v", readErr)
	}
	if string(unchanged) != sentinel {
		t.Errorf("CLAUDE.md = %q, want untouched %q (a failed backup must not allow the overwrite)", unchanged, sentinel)
	}
}
