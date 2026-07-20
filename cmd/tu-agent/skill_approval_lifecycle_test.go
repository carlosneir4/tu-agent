package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestSkillApprovalLifecycle_PendingListsForeignSkill (@s1) verifies that
// `tu-agent memory pending` surfaces a foreign-author, not-yet-approved
// type=skill record — name, author, and revision — so a human knows to
// review it before it can ever reach disk. Today `memory pending` only diffs
// the LOCAL author's own exported chunk against git HEAD; it never looks at
// foreign skill records sitting in the store at all, so this is red until the
// approval-lifecycle pending surface lands.
func TestSkillApprovalLifecycle_PendingListsForeignSkill(t *testing.T) {
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

	var out bytes.Buffer
	memoryPendingCmd.SetOut(&out)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("pending: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "deploy") {
		t.Errorf("want pending output to list %q, got %q", "deploy", got)
	}
	if !strings.Contains(got, "mallory@example.com") {
		t.Errorf("want pending output to name the foreign author %q, got %q", "mallory@example.com", got)
	}
	if !strings.Contains(got, "1") {
		t.Errorf("want pending output to include revision %d, got %q", 1, got)
	}
}

// TestSkillApprovalLifecycle_ApproveMaterializesDurably (@s2) verifies that
// `tu-agent memory approve-skill deploy` writes the foreign record to
// .claude/skills/deploy/SKILL.md immediately, and that the approval is
// durable: a later `memory materialize` run must not undo it. The
// approve-skill command does not exist yet, so this is red: cobra cannot
// resolve "approve-skill" under "memory" and falls back to printing the
// memory command's help (a nil error, no side effect) instead of writing
// anything — the file never appears.
func TestSkillApprovalLifecycle_ApproveMaterializesDurably(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	body := "deploy skill body v1"
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("skill/deploy", body, memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"memory", "approve-skill", "deploy"})
	_ = rootCmd.Execute() // unresolved subcommand today falls back to help with a nil error; the FS assertions below carry the red.

	path := filepath.Join(generatedSkillsDir(repoRoot()), "deploy", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to hold the approved record after approve-skill, got: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content = %q, want %q", got, body)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	got2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to still hold the approved record after a second materialize, got: %v", path, err)
	}
	if string(got2) != body {
		t.Errorf("materialized content after second materialize = %q, want %q (approval must be durable)", got2, body)
	}
}

// TestSkillApprovalLifecycle_HigherRevisionReturnsToPending (@s3) verifies
// that once a skill record is approved at revision 1, importing a
// same-sync-id record at a HIGHER revision (content changed since the human
// approved it) returns it to `memory pending` and materialize must not write
// the new content. The approval precondition is set up directly via
// Store.SetMeta on the "skill_approvals" metadata key — the exact local-only
// JSON-blob shape design.md fixes for this feature — since the approve-skill
// command that would normally produce it does not exist yet. This is red
// because `memory pending` does not look at skill_approvals or foreign skill
// records at all today, so the revision-2 record never appears in its output.
func TestSkillApprovalLifecycle_HigherRevisionReturnsToPending(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	obs1, err := ms.Upsert("skill/deploy", "deploy skill body v1", memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if obs1.Revision != 1 {
		t.Fatalf("test setup: want revision 1 after first upsert, got %d", obs1.Revision)
	}
	// Approval store shape per design.md: metadata key "skill_approvals" holds a
	// JSON object mapping skill name -> approved revision.
	if err := ms.SetMeta("skill_approvals", `{"deploy":1}`); err != nil {
		t.Fatal(err)
	}

	if _, err := ms.ImportRecords([]memory.ChunkRecord{{
		SyncID:   obs1.SyncID,
		TopicKey: "skill/deploy",
		Scope:    "project",
		Title:    "skill/deploy",
		Content:  "deploy skill body v2",
		Type:     "skill",
		Author:   "mallory@example.com",
		Revision: 2,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	memoryPendingCmd.SetOut(&out)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("pending: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "deploy") {
		t.Errorf("want pending output to list the re-imported, higher-revision record %q, got %q", "deploy", got)
	}
	if !strings.Contains(got, "2") {
		t.Errorf("want pending output to show revision %d, got %q", 2, got)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	path := filepath.Join(generatedSkillsDir(repoRoot()), "deploy", "SKILL.md")
	if content, rerr := os.ReadFile(path); rerr == nil && string(content) == "deploy skill body v2" {
		t.Errorf("want materialize NOT to write the unapproved revision-2 content to %s, but it did: %q", path, content)
	}
}

// TestSkillApprovalLifecycle_SameRevisionStaysApproved (@s4) verifies that
// once a skill record is approved at revision 1, re-importing a same-sync-id
// record still at revision 1 leaves the approval intact: `memory pending`
// must not list it, and `memory materialize` must write its content. The
// approval precondition is set up via Store.SetMeta as in @s3. This is red
// today because materialize's author gate skips every foreign-author record
// unconditionally — it has no isApproved branch yet — so the file is never
// written regardless of the skill_approvals state.
func TestSkillApprovalLifecycle_SameRevisionStaysApproved(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	body := "deploy skill body v1"
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	obs1, err := ms.Upsert("skill/deploy", body, memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ms.SetMeta("skill_approvals", `{"deploy":1}`); err != nil {
		t.Fatal(err)
	}

	// Re-import at the SAME revision: ImportRecords skips (revision not
	// higher), so the record and its approval are unchanged.
	if _, err := ms.ImportRecords([]memory.ChunkRecord{{
		SyncID:   obs1.SyncID,
		TopicKey: "skill/deploy",
		Scope:    "project",
		Title:    "skill/deploy",
		Content:  body,
		Type:     "skill",
		Author:   "mallory@example.com",
		Revision: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	memoryPendingCmd.SetOut(&out)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("pending: %v", err)
	}
	if strings.Contains(out.String(), "deploy") {
		t.Errorf("want an approved, same-revision record absent from pending, got %q", out.String())
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	path := filepath.Join(generatedSkillsDir(repoRoot()), "deploy", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to hold the still-approved record's content, got: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content = %q, want %q", got, body)
	}
}

// TestSkillApprovalLifecycle_ApproveRejectsTraversalName (@s5) verifies that
// `memory approve-skill` rejects a traversal-shaped name (a backslash
// segment, mirroring the materialize name guard) with an error, and writes
// nothing under .claude/skills. The command does not exist yet: cobra's
// unresolved-subcommand fallback returns a nil error (it prints the memory
// command's help instead of failing), so the "must fail with an error" half
// of this scenario is red until approve-skill exists and validates its
// argument.
func TestSkillApprovalLifecycle_ApproveRejectsTraversalName(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	name := `a\b`
	rootCmd.SetArgs([]string{"memory", "approve-skill", name})
	err := rootCmd.Execute()
	if err == nil {
		t.Errorf("want approve-skill %q to fail with an error naming the invalid skill name, got nil", name)
	}

	skillsDir := generatedSkillsDir(repoRoot())
	entries, rerr := os.ReadDir(skillsDir)
	if rerr != nil && !os.IsNotExist(rerr) {
		t.Fatalf("read %s: %v", skillsDir, rerr)
	}
	for _, e := range entries {
		t.Errorf("want nothing written under %s for a rejected name, found %s", skillsDir, e.Name())
	}
}
