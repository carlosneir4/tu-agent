package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestImportProvenanceGate_EmptyAuthorSpoofNotMaterialized (@s1) simulates a
// hand-crafted committed chunk containing a type=skill record with an empty
// author, imported via Store.ImportRecords exactly as `memory import` would
// apply it. isLocalAuthor treats an empty author as local, so materialize
// auto-writes this attacker-controlled content to disk today — the
// provenance gate this feature adds must instead recognize the record came
// through ImportRecords (not a local Add/Upsert) and treat it as foreign
// regardless of its (empty) author. RED today: SKILL.md gets written.
func TestImportProvenanceGate_EmptyAuthorSpoofNotMaterialized(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ImportRecords([]memory.ChunkRecord{{
		SyncID:   "sync-empty-author",
		TopicKey: "skill/deploy",
		Scope:    "project",
		Title:    "skill/deploy",
		Content:  "malicious deploy skill body",
		Type:     "skill",
		Author:   "",
		Revision: 1,
	}}); err != nil {
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
		t.Fatalf("want %s absent for an imported record with a spoofed empty author, got stat err=%v", path, err)
	}
}

// TestImportProvenanceGate_SpoofedLocalAuthorNotMaterialized (@s2) simulates
// a hand-crafted chunk record whose author field is forged to equal the
// LOCAL git identity (a victim's own email) — the scenario in the finding
// where ImportRecords preserves the attacker-controlled author verbatim.
// isLocalAuthor compares the record's author string to the local identity
// and finds a match today, so materialize writes it as if it were the
// operator's own local record. The provenance gate must reject this
// regardless of the author string matching, because the record's origin is
// ImportRecords, not a local Add/Upsert. RED today: SKILL.md gets written.
func TestImportProvenanceGate_SpoofedLocalAuthorNotMaterialized(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ImportRecords([]memory.ChunkRecord{{
		SyncID:   "sync-spoofed-author",
		TopicKey: "skill/checkout",
		Scope:    "project",
		Title:    "skill/checkout",
		Content:  "malicious checkout skill body",
		Type:     "skill",
		Author:   "carlos@example.com", // forged to equal the local identity
		Revision: 1,
	}}); err != nil {
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want %s absent for an imported record with a spoofed local author, got stat err=%v", path, err)
	}
}

// TestImportProvenanceGate_LocalUpsertStillMaterializes (@s3) is a green-today
// regression pin: a record created locally via Store.Upsert (never touched
// ImportRecords, empty author because no author was supplied) must still
// materialize after the provenance gate lands — the gate must key off
// import-origin, not merely author emptiness, or it would also lock out
// every genuinely local record.
func TestImportProvenanceGate_LocalUpsertStillMaterializes(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	body := "backup skill body"
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("skill/backup", body, memory.UpsertOpts{
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

	path := filepath.Join(generatedSkillsDir(repoRoot()), "backup", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a local Upsert record to still materialize at %s: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content = %q, want %q", got, body)
	}
}

// TestImportProvenanceGate_ApprovedImportedRecordMaterializes (@s4) is a
// green-today regression pin for the approval escape hatch: an imported
// record with a genuinely foreign author, explicitly approved via
// Store.SetMeta on the skill_approvals metadata key at its exact revision,
// must still materialize. The provenance gate changes how a record's
// foreign-ness is DETECTED (import-origin instead of author string), but
// must not remove the approval path that lets a human deliberately accept a
// teammate's imported skill.
func TestImportProvenanceGate_ApprovedImportedRecordMaterializes(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	body := "release skill body"
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ImportRecords([]memory.ChunkRecord{{
		SyncID:   "sync-approved-import",
		TopicKey: "skill/release",
		Scope:    "project",
		Title:    "skill/release",
		Content:  body,
		Type:     "skill",
		Author:   "mallory@example.com",
		Revision: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := ms.SetMeta("skill_approvals", `{"release":1}`); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	path := filepath.Join(generatedSkillsDir(repoRoot()), "release", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the explicitly approved imported record to materialize at %s: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content = %q, want %q", got, body)
	}
}
