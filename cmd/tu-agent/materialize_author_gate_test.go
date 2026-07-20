package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestMaterializeAuthorGate_ForeignAuthorNotWritten (@s1) verifies that a
// skill record authored by someone other than the local git identity is
// skipped by materialize entirely — it must never reach disk.
func TestMaterializeAuthorGate_ForeignAuthorNotWritten(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("skill/deploy", "deploy skill body", memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	path := filepath.Join(generatedSkillsDir(repoRoot()), "deploy", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want %s absent for a foreign-author record, got stat err=%v", path, err)
	}
}

// TestMaterializeAuthorGate_LocalAuthorStillMaterializes (@s2) verifies that
// the author gate does not regress the existing behavior: a record authored
// by the local git identity still materializes with its content intact.
func TestMaterializeAuthorGate_LocalAuthorStillMaterializes(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	body := "checkout skill body"
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("skill/checkout", body, memory.UpsertOpts{
		Type:   "skill",
		Author: "carlos@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	path := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected materialized skill at %s: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content = %q, want %q", got, body)
	}
}

// TestMaterializeAuthorGate_EmptyLocalIdentityFailsClosed (@s3) verifies that
// when no local git identity is configured at all, materialize treats every
// authored record as foreign and writes nothing — fail closed, not fail
// open. The test isolates HOME/global/system git config so the host
// machine's real user.email can never leak into the check.
func TestMaterializeAuthorGate_EmptyLocalIdentityFailsClosed(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	// Deliberately no `git config user.email` in the repo-local config.

	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	t.Chdir(dir)

	if got := gitAuthor(); got != "" {
		t.Fatalf("test setup: gitAuthor() = %q, want empty (isolation failed)", got)
	}

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("skill/deploy", "deploy skill body", memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	path := filepath.Join(generatedSkillsDir(repoRoot()), "deploy", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want %s absent with no local git identity configured, got stat err=%v", path, err)
	}
}

// TestMaterializeAuthorGate_BackslashNameRejected (@s4) verifies that the
// materialize name guard rejects a topic key whose name segment contains a
// backslash. Backslash is a legal filename character on darwin, so the
// pre-fix guard (which only rejects "", ".", "..", and "/") happily creates a
// directory literally named `a\b` — this test asserts no such path is ever
// created.
func TestMaterializeAuthorGate_BackslashNameRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert(`skill/a\b`, "malicious-name skill body", memory.UpsertOpts{
		Type: "skill",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	skillsDir := generatedSkillsDir(repoRoot())
	entries, err := os.ReadDir(skillsDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read %s: %v", skillsDir, err)
	}
	for _, e := range entries {
		if e.Name() == `a\b` {
			t.Fatalf("want no entry named `a\\b` under %s, found one (IsDir=%v)", skillsDir, e.IsDir())
		}
	}
	path := filepath.Join(skillsDir, `a\b`, "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want %s absent, got stat err=%v", path, err)
	}
}
