package main

// RED-phase test for feature `reconcile-name-sanitize` (@f0-1) — scenario @s5
// of
//   .tu-agent/tdd/f0-security-hardening-fixes-plan-version-es/features/reconcile-name-sanitize.feature
//
// @s5 pins that the shared `applyReconcile` adapter (cmd/tu-agent/memory.go)
// — the single funnel both the CLI `reconcile --name` flag and the MCP
// `mem_reconcile` tool call into — propagates the ApplyPlanWithOptions
// rejection unchanged, and that no filesystem mutation occurs. Both surfaces
// share this one adapter (§10 parity), so exercising it directly covers both
// without needing live CLI/MCP wiring.
//
// Reuses seedCluster / seedOrphanSkill from crystallize_gen_test.go /
// crystallize_orphan_cli_test.go and repoRoot / memoryDBPath / generatedSkillsDir
// from memory.go.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/reconcile"
)

// @s5 — the shared applyReconcile adapter propagates a traversal rejection
// from ApplyPlanWithOptions unchanged, and no filesystem mutation occurs.
func TestApplyReconcile_PropagatesNameTraversalRejection(t *testing.T) {
	t.Chdir(t.TempDir())

	seedCluster(t) // one live "checkout" cluster (3 notes)
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	skillsDir := generatedSkillsDir(repoRoot())
	// Materialize the crystallize-managed source folder so a bug that skipped
	// the guard would actually attempt (and this test would catch) a rename.
	oldDir := filepath.Join(skillsDir, "acme-orphan-a")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	markedBody := "<!-- " + crystallize.Marker + " source-hash=deadbeef label=acme-orphan-a -->\n---\nname: acme-orphan-a\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(oldDir, "SKILL.md"), []byte(markedBody), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	before, ok := reconcileRecordFromStore(t, s, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("seeded orphan skill/acme-orphan-a not found")
	}

	_, err = applyReconcile(s, skillsDir, 3, "skill/acme-orphan-a",
		reconcile.ApplyOptions{Name: "../evil", ToCluster: "checkout"})
	if err == nil {
		t.Fatalf("applyReconcile must propagate the ApplyPlanWithOptions traversal rejection")
	}

	// No filesystem mutation: the source folder is still present, untouched.
	if _, statErr := os.Stat(filepath.Join(oldDir, "SKILL.md")); statErr != nil {
		t.Errorf("source skill folder was mutated/removed despite the rejected Name: %v", statErr)
	}
	// No folder was created outside skillsDir via the traversal.
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(skillsDir), "evil")); statErr == nil {
		t.Errorf("a traversal folder was created outside skillsDir")
	}

	// The record itself is untouched (no rebind/rename fired).
	after, ok := reconcileRecordFromStore(t, s, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("skill/acme-orphan-a vanished after the rejected apply")
	}
	if after.Content != before.Content || after.Revision != before.Revision {
		t.Errorf("record mutated despite the rejected Name:\n before = %+v\n after  = %+v", before, after)
	}
	if _, ok := reconcileRecordFromStore(t, s, "skill/evil"); ok {
		t.Errorf("skill/evil was created despite the rejected Name")
	}
}

// reconcileRecordFromStore returns the live observation for topic from an
// already-open store, and whether it exists. Distinct from reconcile_apply_test.go's
// reconcileRecord, which opens its own store handle — this test needs the SAME
// handle applyReconcile is called with, to observe the pre/post state without a
// second open racing SQLite's locking.
func reconcileRecordFromStore(t *testing.T, s *memory.Store, topic string) (memory.Observation, bool) {
	t.Helper()
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if o.TopicKey == topic {
			return o, true
		}
	}
	return memory.Observation{}, false
}
