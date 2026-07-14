package reconcile

// RED-phase tests for feature `reconcile-prune-gate` (@f0-2) — scenarios
// @s1..@s3 of
//   .tu-agent/tdd/f0-security-hardening-fixes-plan-version-es/features/reconcile-prune-gate.feature
//
// reconcileFolders (rename.go:218) currently os.RemoveAll's any
// crystallize-MARKED folder under skillsDir with no live backing record,
// unconditionally, on every ApplyPlanWithOptions call — even though
// applyReconcile's own doc comment (cmd/tu-agent/memory.go) claims "auto-applies
// nothing". This feature gates that removal behind a new ApplyOptions.PruneFolders
// bool: false (the default) is a dry-run — candidate folders are SCANNED and
// REPORTED but never removed; true reproduces today's removal behavior.
// ApplyResult gains a WouldRemove []string field to report dry-run candidates,
// parallel to the existing Removed []string field for actual removals.
//
// Production API this file expects (to be ADDED to internal/reconcile,
// internal/reconcile/rename.go and internal/reconcile/apply.go):
//
//   type ApplyOptions struct {
//       ...                    // existing Name, ToCluster fields (rename.go)
//       PruneFolders bool      // --prune-folders / prune_folders: gate folder removal
//   }
//
//   type ApplyResult struct {
//       ...                    // existing Rebound, Renamed, Divergent, Removed, Skipped
//       WouldRemove []string   // marked-but-orphaned folder names reported, NOT
//                              // removed, when PruneFolders == false
//   }
//
// PruneFolders is the ONLY new field on ApplyOptions and WouldRemove the only
// new field on ApplyResult these tests reference, so the RED failure is a clean
// compile error isolated to internal/reconcile — sibling packages still build.
//
// Reuses helpers from apply_test.go / rename_test.go (same package): newStore,
// materializeSkill, isDir. §9: generic acme-* fixtures.

import (
	"os"
	"path/filepath"
	"testing"
)

// @s1 — without the flag, an orphaned marked folder survives on disk and is
// reported under a "would remove" list; nothing is actually removed.
func TestApplyPlanWithOptions_PruneFoldersFalse_OrphanSurvivesAndReported(t *testing.T) {
	store := newStore(t)
	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "acme-orphaned-folder", true) // marked, no live record

	res, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{PruneFolders: false})
	if err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	if !isDir(filepath.Join(skillsDir, "acme-orphaned-folder")) {
		t.Fatalf("orphaned marked folder was removed under the default (PruneFolders=false) dry-run gate")
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want empty under PruneFolders=false (nothing must actually be removed)", res.Removed)
	}
	found := false
	for _, name := range res.WouldRemove {
		if name == "acme-orphaned-folder" {
			found = true
		}
	}
	if !found {
		t.Errorf("WouldRemove = %v, want it to include %q", res.WouldRemove, "acme-orphaned-folder")
	}
}

// @s2 — with ApplyOptions.PruneFolders = true (the field --prune-folders /
// prune_folders threads into), the orphaned marked folder is removed from disk
// and reported under Removed, not WouldRemove.
func TestApplyPlanWithOptions_PruneFoldersTrue_OrphanRemoved(t *testing.T) {
	store := newStore(t)
	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "acme-orphaned-folder", true) // marked, no live record

	res, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{PruneFolders: true})
	if err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	if isDir(filepath.Join(skillsDir, "acme-orphaned-folder")) {
		t.Fatalf("orphaned marked folder still present on disk after PruneFolders=true")
	}
	found := false
	for _, name := range res.Removed {
		if name == "acme-orphaned-folder" {
			found = true
		}
	}
	if !found {
		t.Errorf("Removed = %v, want it to include %q", res.Removed, "acme-orphaned-folder")
	}
	for _, name := range res.WouldRemove {
		if name == "acme-orphaned-folder" {
			t.Errorf("WouldRemove = %v, must not also list a folder that was actually removed", res.WouldRemove)
		}
	}
}

// @s3 — SMOKE CHECK ONLY (deep coverage already exists in
// TestApplyRename_FolderReconciliation and
// TestApplyRename_HandWrittenSourceFolderReportsDivergence in rename_test.go): a
// hand-written (unmarked) folder with no live record is never touched, byte for
// byte, in either PruneFolders mode.
func TestApplyPlanWithOptions_PruneFoldersGate_HandWrittenFolderNeverTouched(t *testing.T) {
	store := newStore(t)
	skillsDir := t.TempDir()
	manualPath := materializeSkill(t, skillsDir, "acme-manual", false) // hand-written, no marker, no record
	manualBefore, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{PruneFolders: false}); err != nil {
		t.Fatalf("ApplyPlanWithOptions (PruneFolders=false): %v", err)
	}
	if !isDir(filepath.Join(skillsDir, "acme-manual")) {
		t.Fatalf("hand-written folder removed under PruneFolders=false; must never be touched")
	}

	if _, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{PruneFolders: true}); err != nil {
		t.Fatalf("ApplyPlanWithOptions (PruneFolders=true): %v", err)
	}
	if !isDir(filepath.Join(skillsDir, "acme-manual")) {
		t.Fatalf("hand-written folder removed under PruneFolders=true; must never be touched")
	}
	manualAfter, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(manualAfter) != string(manualBefore) {
		t.Errorf("hand-written SKILL.md changed across the two runs:\n before = %q\n after  = %q", manualBefore, manualAfter)
	}
}
