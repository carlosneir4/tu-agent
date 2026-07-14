package main

// RED-phase test for feature `reconcile-prune-gate` (@f0-2) — scenario @s4 of
//   .tu-agent/tdd/f0-security-hardening-fixes-plan-version-es/features/reconcile-prune-gate.feature
//
// @s4 pins that the shared `applyReconcile` adapter (cmd/tu-agent/memory.go) —
// the single funnel both the CLI `reconcile --apply --prune-folders` flag and
// the MCP `mem_reconcile` tool's `prune_folders` arg call into — threads
// reconcile.ApplyOptions.PruneFolders through to ApplyPlanWithOptions
// unchanged: default (PruneFolders omitted/false) removes nothing and reports
// the orphaned marked folder as a removal candidate; explicit
// PruneFolders:true removes it. Exercised at the adapter directly, same
// pattern as reconcile_name_sanitize_test.go's @s5 — both CLI and MCP share
// this one function, so covering it here covers both surfaces per §10 parity.
//
// Reuses seedCluster from crystallize_gen_test.go and repoRoot / memoryDBPath /
// generatedSkillsDir from memory.go.
//
// Production API this file expects (to be ADDED to internal/reconcile, see
// internal/reconcile/prune_gate_test.go for the full contract):
//   reconcile.ApplyOptions gains a PruneFolders bool field.
// This is the ONLY new symbol this file references, so the RED failure is a
// clean compile error (an unknown field on the ApplyOptions struct literal).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/reconcile"
)

// materializeMarkedOrphanFolder writes a crystallize-MARKED SKILL.md under
// skillsDir/<name>/ with no backing live record, so reconcileFolders treats it
// as a removal candidate. Mirrors the marked-source-folder setup in
// reconcile_name_sanitize_test.go.
func materializeMarkedOrphanFolder(t *testing.T, skillsDir, name string) string {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "<!-- " + crystallize.Marker + " source-hash=deadbeef label=" + name + " -->\n---\nname: " + name + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// @s4 (default leg) — neither the CLI `--prune-folders` flag nor the MCP
// `prune_folders` arg is passed, so ApplyOptions.PruneFolders is the zero value
// (false). applyReconcile's own doc comment claims "auto-applies nothing"; this
// pins that the orphaned marked folder actually survives and is reported as a
// would-remove candidate, not silently dropped from the report.
func TestApplyReconcile_PruneFoldersDefault_NoRemoval(t *testing.T) {
	t.Chdir(t.TempDir())

	seedCluster(t) // one live "checkout" cluster, unrelated to the orphaned folder below

	skillsDir := generatedSkillsDir(repoRoot())
	orphanDir := materializeMarkedOrphanFolder(t, skillsDir, "acme-prune-candidate")

	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	text, err := applyReconcile(s, skillsDir, 3, "", reconcile.ApplyOptions{})
	if err != nil {
		t.Fatalf("applyReconcile: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(orphanDir, "SKILL.md")); statErr != nil {
		t.Errorf("orphaned marked folder was removed under the default (no prune flag/arg) gate: %v", statErr)
	}
	if !strings.Contains(text, "acme-prune-candidate") {
		t.Fatalf("rendered apply output does not mention the orphaned folder at all:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "would remove") {
		t.Errorf("rendered apply output does not report a would-remove candidate:\n%s", text)
	}
}

// @s4 (explicit leg) — ApplyOptions.PruneFolders: true is the field both
// `--prune-folders` (CLI) and `prune_folders: true` (MCP) thread into via this
// shared adapter; passing it removes the folder and reports the removal.
func TestApplyReconcile_PruneFoldersExplicitTrue_Removes(t *testing.T) {
	t.Chdir(t.TempDir())

	seedCluster(t)

	skillsDir := generatedSkillsDir(repoRoot())
	orphanDir := materializeMarkedOrphanFolder(t, skillsDir, "acme-prune-candidate")

	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	text, err := applyReconcile(s, skillsDir, 3, "", reconcile.ApplyOptions{PruneFolders: true})
	if err != nil {
		t.Fatalf("applyReconcile: %v", err)
	}

	if _, statErr := os.Stat(orphanDir); statErr == nil {
		t.Errorf("orphaned marked folder still present after ApplyOptions.PruneFolders: true")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("unexpected stat error checking removed folder: %v", statErr)
	}
	if !strings.Contains(text, "acme-prune-candidate") {
		t.Fatalf("rendered apply output does not mention the removed folder at all:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "removed") {
		t.Errorf("rendered apply output does not report the removal:\n%s", text)
	}
}
