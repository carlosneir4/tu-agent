package main

// RED-phase tests for feature `reconcile-apply-wiring` (B2) — wire the reconcile
// APPLY path to BOTH surfaces (§10 dual-availability) over the already-built
// core in internal/reconcile (ApplyPlanWithOptions / RenderApplyResult).
//
// -----------------------------------------------------------------------------
// CONTRACT FOR THE IMPLEMENTER (documented per the task) — pin these exactly:
//
// New flags on `memory reconcile` (mirroring memory rescope / memory delete):
//   --apply        bool    turn on apply mode (absent => dry-run, existing)
//   --topic        string  SELECTOR: scope the op to ONE orphan by topic key
//                          (e.g. skill/acme-orphan-a). Chosen to mirror the
//                          --topic selector that `memory rescope` and
//                          `memory delete` already use to name a record.
//   --to-cluster   string  force the re-point target cluster label
//   --name         string  rename skill/<old> -> skill/<new> (record + folder)
//   --min          int     existing (cluster-size threshold; default 5)
//
// Behavior these tests pin:
//   - no --apply                    => dry-run, mutate nothing (existing).
//   - --apply (bare)                => runs the apply path but rebinds NOTHING in
//                                      this branch: member-set storage is DEFERRED
//                                      to Leg 4, so PlanFrom feeds Suggest(nil,..),
//                                      every orphan has EMPTY candidates,
//                                      AutoApplyTarget never fires, and all orphans
//                                      land in ApplyResult.Skipped (untouched).
//                                      Bulk auto-apply is intentionally inert here.
//   - --apply --topic X --to-cluster L
//                                    => rebind ONLY orphan X to cluster L.
//   - --apply --topic X --to-cluster L --name N
//                                    => rebind + rename skill/X -> skill/N.
//   - --to-cluster / --name WITHOUT --topic   => ERROR, mutate nothing.
//   - --topic / --to-cluster / --name WITHOUT --apply => ERROR, mutate nothing.
//
// Recommended shared adapter both surfaces call (returns RenderApplyResult text):
//   func applyReconcile(s *memory.Store, skillsDir string, minSize int,
//       selectorTopic string, opts reconcile.ApplyOptions) (string, error)
//   (the task's sketch omitted the selector; it is needed to scope a single
//    orphan — carry it as an extra param OR filter plan.Orphans before the call.)
//
// MCP `mem_reconcile` (memReconcileMCPInput) gains fields:
//   Apply bool `json:"apply"`, Topic string `json:"topic"`,
//   ToCluster string `json:"to_cluster"`, Name string `json:"name"`.
// -----------------------------------------------------------------------------
//
// RED SOURCE: the new --apply/--topic/--to-cluster/--name flags and the
// memReconcileMCPInput.{Apply,Topic,ToCluster,Name} fields do not exist yet, so
// this file (and mcp_reconcile_test.go's additions) FAIL TO COMPILE / FAIL the
// flag-existence assertion — the correct RED, scoped to cmd/tu-agent test files.
//
// NOTE ON test 1 (bare --apply, no member-sets): per the spec's LOCKED
// decisions, member-set storage is DEFERRED to Leg 4. reconcile.PlanFrom
// deliberately calls Suggest(nil, clusters), so every surface-built orphan plan
// carries EMPTY candidates and AutoApplyTarget can never fire in this branch.
// Bare `--apply` therefore rebinds NOTHING: every orphan is reported as Skipped
// and the store is left untouched. The functional apply path in THIS leg is the
// HUMAN-CONFIRMED one (--topic + --to-cluster / --name, tests 2-3). test-1 pins
// that honest inert behavior; bulk auto-apply stays dormant until Leg 4 stores
// member-sets. This test does NOT require deferred member-recovery — its only
// RED source is the not-yet-registered --apply flag.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

// setReconcileFlag sets a flag on the shared memoryReconcileCmd by name. It
// fails loudly when the flag is not registered yet — the RED signal that the
// implementer still has to add it.
func setReconcileFlag(t *testing.T, name, val string) {
	t.Helper()
	if err := memoryReconcileCmd.Flags().Set(name, val); err != nil {
		t.Fatalf("set --%s: %v (implementer must register --%s on `memory reconcile`)", name, err, name)
	}
}

// resetReconcileState restores the shared command's flag state and the package
// min global after a test, since memoryReconcileCmd is a process-global.
func resetReconcileState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		memoryReconcileCmd.SetOut(nil)
		memReconcileMin = 5
		for _, name := range []string{"apply", "topic", "to-cluster", "name"} {
			if fl := memoryReconcileCmd.Flags().Lookup(name); fl != nil {
				_ = memoryReconcileCmd.Flags().Set(name, fl.DefValue)
			}
		}
	})
}

// runReconcileApply drives the CLI command and returns its stdout text.
func runReconcileApply(t *testing.T) (string, error) {
	t.Helper()
	var buf strings.Builder
	memoryReconcileCmd.SetOut(&buf)
	err := memoryReconcileCmd.RunE(memoryReconcileCmd, nil)
	return buf.String(), err
}

// reconcileRecord returns the live observation for topic, and whether it exists.
func reconcileRecord(t *testing.T, topic string) (memory.Observation, bool) {
	t.Helper()
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
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

// isOrphanTopic reports whether the skill record at topic classifies as an orphan
// against the live clusters detected at min.
func isOrphanTopic(t *testing.T, topic string, min int) bool {
	t.Helper()
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]crystallize.Cluster{}
	for _, c := range crystallize.Detect(obs, min) {
		byLabel[c.Label] = c
	}
	for _, o := range obs {
		if o.TopicKey == topic {
			return crystallize.RecordStatus(o, byLabel) == crystallize.StatusOrphan
		}
	}
	t.Fatalf("record %q not found", topic)
	return false
}

// materializeMarkedSkill writes a crystallize-managed SKILL.md folder so the
// apply path treats it as a managed folder eligible for rename.
func materializeMarkedSkill(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(generatedSkillsDir(repoRoot()), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "<!-- " + crystallize.Marker + " source-hash=deadbeef label=" + name + " -->\n---\nname: " + name + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// @test-1 — bare `--apply` with NO stored member-sets auto-applies NOTHING.
// Member-set storage is DEFERRED to Leg 4 (spec LOCKED decision), so
// reconcile.PlanFrom feeds Suggest(nil, clusters): every orphan carries EMPTY
// candidates, AutoApplyTarget never fires, and all orphans land in
// ApplyResult.Skipped. The store is left untouched (both records still classify
// as orphans afterward) and the render is the "0 rebound" apply summary with the
// trailing "orphan left untouched" line naming both topics — NOT a dry-run plan.
// This pins the honest in-scope behavior; bulk auto-apply is intentionally inert
// in this branch until Leg 4 stores member-sets.
func TestReconcileApply_BareApplyNoMemberSets_AutoAppliesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t) // one live "checkout" cluster (3 notes)
	seedOrphanSkill(t, "acme-orphan-one", "acme-ghost-one")
	seedOrphanSkill(t, "acme-orphan-two", "acme-ghost-two")

	setReconcileFlag(t, "apply", "true")

	out, err := runReconcileApply(t)
	if err != nil {
		t.Fatalf("memory reconcile --apply: %v", err)
	}

	// Output is the apply result, not a dry-run plan.
	if !strings.Contains(out, "Applied reconcile") {
		t.Errorf("bare --apply output is not RenderApplyResult text:\n%s", out)
	}
	if strings.Contains(out, "Reconcile plan") {
		t.Errorf("bare --apply printed a dry-run plan instead of applying:\n%s", out)
	}
	// Nothing was rebound/renamed: the summary header reports zero of each.
	if !strings.Contains(out, "0 rebound, 0 renamed, 0 divergence") {
		t.Errorf("bare --apply should rebind nothing (no stored member-sets, Leg-4 deferral):\n%s", out)
	}
	// Both orphans are reported as left untouched (Skipped), naming each topic.
	if !strings.Contains(out, "orphan left untouched") {
		t.Errorf("output missing the 'orphan left untouched' line for skipped orphans:\n%s", out)
	}
	for _, topic := range []string{"skill/acme-orphan-one", "skill/acme-orphan-two"} {
		// Nothing mutated: each record still classifies as an orphan.
		if !isOrphanTopic(t, topic, 3) {
			t.Errorf("%s no longer an orphan after bare --apply; nothing should have been rebound", topic)
		}
		// Its bound label is unchanged (still the ghost label it was seeded with).
		rec, ok := reconcileRecord(t, topic)
		if !ok {
			t.Fatalf("%s vanished after bare --apply", topic)
		}
		wantLabel := "acme-ghost-" + strings.TrimPrefix(topic, "skill/acme-orphan-")
		if got := crystallize.ParseLabel(rec.Content); got != wantLabel {
			t.Errorf("%s label = %q, want it untouched at %q", topic, got, wantLabel)
		}
		// The skipped orphan is still named in the output.
		if !strings.Contains(out, topic) {
			t.Errorf("output missing the skipped orphan %s:\n%s", topic, out)
		}
	}
}

// @test-2 — `--apply --topic X --to-cluster L` rebinds ONLY orphan X to L and
// leaves the other orphan untouched (the selector scopes the op to one record).
func TestReconcileApply_SelectorToClusterScopesOneOrphan(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t) // live "checkout" cluster
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")
	seedOrphanSkill(t, "acme-orphan-b", "acme-ghost-b")

	setReconcileFlag(t, "apply", "true")
	setReconcileFlag(t, "topic", "skill/acme-orphan-a")
	setReconcileFlag(t, "to-cluster", "checkout")

	out, err := runReconcileApply(t)
	if err != nil {
		t.Fatalf("memory reconcile --apply --topic --to-cluster: %v", err)
	}

	// Only orphan A is rebound (to checkout) and no longer an orphan.
	if isOrphanTopic(t, "skill/acme-orphan-a", 3) {
		t.Errorf("skill/acme-orphan-a not rebound; still an orphan")
	}
	recA, _ := reconcileRecord(t, "skill/acme-orphan-a")
	if got := crystallize.ParseLabel(recA.Content); got != "checkout" {
		t.Errorf("skill/acme-orphan-a label = %q, want it forced to %q", got, "checkout")
	}
	// Orphan B is untouched: still bound to its ghost label, still an orphan.
	recB, _ := reconcileRecord(t, "skill/acme-orphan-b")
	if got := crystallize.ParseLabel(recB.Content); got != "acme-ghost-b" {
		t.Errorf("skill/acme-orphan-b label changed to %q; the selector must not touch it", got)
	}
	if !isOrphanTopic(t, "skill/acme-orphan-b", 3) {
		t.Errorf("skill/acme-orphan-b should still be an orphan; the selector scoped to A only")
	}
	if strings.Contains(out, "skill/acme-orphan-b") {
		t.Errorf("output names the unselected orphan B:\n%s", out)
	}
}

// @test-3 — `--apply --topic X --to-cluster L --name N` renames skill/X ->
// skill/N: the store record moves and the crystallize-managed folder is renamed.
func TestReconcileApply_RenameMovesRecordAndFolder(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t) // live "checkout" cluster
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")
	materializeMarkedSkill(t, "acme-orphan-a") // managed folder to rename

	setReconcileFlag(t, "apply", "true")
	setReconcileFlag(t, "topic", "skill/acme-orphan-a")
	setReconcileFlag(t, "to-cluster", "checkout")
	setReconcileFlag(t, "name", "acme-new")

	out, err := runReconcileApply(t)
	if err != nil {
		t.Fatalf("memory reconcile --apply ... --name: %v", err)
	}

	// The record moved: skill/acme-new exists, skill/acme-orphan-a is gone.
	if _, ok := reconcileRecord(t, "skill/acme-new"); !ok {
		t.Errorf("renamed record skill/acme-new not found in the store")
	}
	if _, ok := reconcileRecord(t, "skill/acme-orphan-a"); ok {
		t.Errorf("old record skill/acme-orphan-a still present after rename")
	}
	// The managed folder moved on disk.
	base := generatedSkillsDir(repoRoot())
	if _, err := os.Stat(filepath.Join(base, "acme-new", "SKILL.md")); err != nil {
		t.Errorf("renamed skill folder acme-new/SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "acme-orphan-a")); !os.IsNotExist(err) {
		t.Errorf("old skill folder acme-orphan-a still exists after rename (err=%v)", err)
	}
	if !strings.Contains(out, "renamed") || !strings.Contains(out, "skill/acme-new") {
		t.Errorf("output missing the rename line:\n%s", out)
	}
}

// @test-4 — `--name` WITHOUT the selector errors and mutates nothing (applying a
// single --name across multiple orphans would collide).
func TestReconcileApply_NameWithoutSelectorErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t)
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	setReconcileFlag(t, "apply", "true")
	setReconcileFlag(t, "name", "acme-new") // no --topic selector

	_, err := runReconcileApply(t)
	if err == nil {
		t.Fatalf("expected an error: --name requires the --topic selector")
	}
	// Nothing mutated: the orphan is untouched and no renamed record exists.
	rec, ok := reconcileRecord(t, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("skill/acme-orphan-a vanished after a rejected --name")
	}
	if got := crystallize.ParseLabel(rec.Content); got != "acme-ghost-a" {
		t.Errorf("orphan label changed to %q on a rejected --name; must be untouched", got)
	}
	if _, ok := reconcileRecord(t, "skill/acme-new"); ok {
		t.Errorf("skill/acme-new was created despite the error")
	}
}

// @test-5 — a selector/target flag set WITHOUT --apply errors (avoid a silent
// no-op) and mutates nothing.
func TestReconcileApply_TargetingWithoutApplyErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t)
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	// --topic + --to-cluster but NO --apply.
	setReconcileFlag(t, "topic", "skill/acme-orphan-a")
	setReconcileFlag(t, "to-cluster", "checkout")

	_, err := runReconcileApply(t)
	if err == nil {
		t.Fatalf("expected an error: targeting flags without --apply")
	}
	rec, ok := reconcileRecord(t, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("skill/acme-orphan-a vanished after a rejected op")
	}
	if got := crystallize.ParseLabel(rec.Content); got != "acme-ghost-a" {
		t.Errorf("orphan label changed to %q without --apply; must be untouched", got)
	}
}

// @test-6 — no --apply is dry-run: it reports the plan and mutates neither the
// store rows nor the skill files.
func TestReconcileApply_NoApplyIsDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	resetReconcileState(t)
	memReconcileMin = 3

	seedCluster(t)
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	before, _ := reconcileRecord(t, "skill/acme-orphan-a")

	out, err := runReconcileApply(t)
	if err != nil {
		t.Fatalf("memory reconcile (dry-run): %v", err)
	}
	if !strings.Contains(out, "Reconcile plan") {
		t.Errorf("dry-run did not print a plan:\n%s", out)
	}
	if strings.Contains(out, "Applied reconcile") {
		t.Errorf("dry-run applied changes:\n%s", out)
	}
	after, ok := reconcileRecord(t, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("skill/acme-orphan-a vanished under dry-run")
	}
	if after.Content != before.Content || after.Revision != before.Revision {
		t.Errorf("dry-run mutated the record:\n before=%+v\n after=%+v", before, after)
	}
}
