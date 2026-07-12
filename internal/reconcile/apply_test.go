package reconcile

// RED-phase tests for feature `reconcile-rebind-apply` (leg 5, feature 2 of 4) —
// scenarios @s1..@s5 of
//   .tu-agent/tdd/crystallize-community-detection-clusteri/features/reconcile-rebind-apply.feature
//
// This is the DEFAULT `--apply` behaviour: rebind-only (chunk-clean,
// sync_id-stable). It rewrites an orphan's provenance (new label=, recomputed
// source-hash) via Upsert on the SAME topic key, so sync_id is unchanged and the
// shared chunk stays clean. Bare `--apply` writes ONLY the unambiguous case
// (exactly one candidate cluster clearing overlap >= 0.5, per AutoApplyTarget);
// ambiguous / low-overlap orphans stay visibly orphaned. Idempotent. Respects
// MaterializeDecision (a hand-written SKILL.md is never clobbered — the record is
// updated and the divergence is reported).
//
// These reference the apply CORE the implementer will ADD to package reconcile,
// so they FAIL TO COMPILE until it exists — the correct RED, ISOLATED to
// internal/reconcile (sibling legs in internal/crystallize, internal/memory, and
// cmd/tu-agent still build). They are white-box: they exercise the core directly
// against a real temp *memory.Store + a temp skills dir, and INJECT the plan +
// live clusters (feature-1 style), so the assertions do NOT depend on the
// deferred candidate-suggestion heuristic (member-set storage is leg 4) — only on
// the apply MECHANICS this feature delivers.
//
// Production API this file expects (to be ADDED to internal/reconcile):
//
//   // ReboundAction is one rebind an apply run performed: the record's
//   // (unchanged) topic key, its previous bound label, and the live cluster
//   // label it now binds to.
//   type ReboundAction struct { Topic, OldLabel, NewLabel string }
//
//   // Divergence flags a rebound record whose on-disk SKILL.md was left
//   // byte-for-byte intact because it is hand-written (no crystallize marker):
//   // the record's provenance was updated but the file was not.
//   type Divergence struct { Topic, Path string }
//
//   // ApplyResult reports what a rebind-only apply changed.
//   type ApplyResult struct {
//       Rebound   []ReboundAction
//       Divergent []Divergence
//   }
//
//   // ApplyPlan executes the rebind-only reconcile for a pre-computed plan
//   // against the store + skills dir. For each orphan it consults
//   // AutoApplyTarget(orphan.Candidates); ONLY when that yields a single target
//   // does it rebind — reading the record's current content, rewriting the
//   // provenance line to label=<target> with source-hash recomputed against the
//   // matched cluster's members (looked up in clusters by label), and persisting
//   // via Upsert on the SAME topic key (sync_id stable). Ambiguous / no-target
//   // orphans, and records whose current label already matches a live cluster
//   // (already reconciled), are left untouched. The on-disk folder is the
//   // record's existing skill name (TrimPrefix topic "skill/"): a marked/absent
//   // SKILL.md may be re-materialized, but a hand-written one
//   // (MaterializeDecision == false) is preserved and reported as a Divergence.
//   // Idempotent: a second call finds nothing to change.
//   func ApplyPlan(store *memory.Store, plan Plan, clusters []crystallize.Cluster, skillsDir string) (ApplyResult, error)
//
// Fixtures are generic and fictional (acme-*) per repo §9. Helpers mem, clus,
// orphanRecord, snapshotStore, snapshotFiles are reused from plan_test.go.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

// newStore opens a fresh temp store, closed on cleanup.
func newStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedOrphan upserts a skill record whose provenance label matches no live
// cluster, so it reads as an orphan.
func seedOrphan(t *testing.T, s *memory.Store, name, label string) {
	t.Helper()
	o := orphanRecord(name, label, []memory.Observation{mem("reference/" + label)})
	if _, err := s.Upsert(o.TopicKey, o.Content, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}
}

// getRecord returns the live observation for a topic key, failing if absent.
func getRecord(t *testing.T, s *memory.Store, topic string) memory.Observation {
	t.Helper()
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if o.TopicKey == topic {
			return o
		}
	}
	t.Fatalf("record %q not found in store", topic)
	return memory.Observation{}
}

// byLabelMap indexes clusters by their label for RecordStatus.
func byLabelMap(cs []crystallize.Cluster) map[string]crystallize.Cluster {
	m := make(map[string]crystallize.Cluster, len(cs))
	for _, c := range cs {
		m[c.Label] = c
	}
	return m
}

func hasDivergence(ds []Divergence, topic string) bool {
	for _, d := range ds {
		if d.Topic == topic {
			return true
		}
	}
	return false
}

// @s1 — the unambiguous orphan is rebound and its status recovers. An orphan
// with exactly one candidate cluster clearing overlap >= 0.5 is rewritten: its
// provenance label= becomes that cluster's current label, its source-hash is
// recomputed against the matched cluster's members, and it thereafter classifies
// as [skill] (StatusCurrent), no longer [orphan].
func TestApplyPlan_UnambiguousOrphanRebound(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-orphan", "acme-ghost")

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}

	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/acme-orphan",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	res, err := ApplyPlan(store, plan, clusters, t.TempDir())
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	// The rebind is reported as a single action with the before/after labels.
	if len(res.Rebound) != 1 {
		t.Fatalf("Rebound = %d action(s), want exactly 1: %+v", len(res.Rebound), res.Rebound)
	}
	if a := res.Rebound[0]; a.Topic != "skill/acme-orphan" || a.OldLabel != "acme-ghost" || a.NewLabel != "acme-checkout" {
		t.Errorf("Rebound[0] = %+v, want {skill/acme-orphan acme-ghost acme-checkout}", a)
	}

	rec := getRecord(t, store, "skill/acme-orphan")

	// Provenance label= is set to the matched cluster's current label.
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("rebound label = %q, want %q", got, "acme-checkout")
	}
	// Source-hash is recomputed against the matched cluster's members.
	wantHash := crystallize.SourceHash(target.Members)
	if got := crystallize.ParseSourceHash(rec.Content); got != wantHash {
		t.Errorf("rebound source-hash = %q, want SourceHash(matched members) = %q", got, wantHash)
	}
	// The record no longer classifies as [orphan]; it recovers to [skill]/[stale].
	st := crystallize.RecordStatus(rec, byLabelMap(clusters))
	if st == crystallize.StatusOrphan {
		t.Errorf("rebound record still classifies StatusOrphan")
	}
	if st != crystallize.StatusCurrent && st != crystallize.StatusStale {
		t.Errorf("rebound record status = %v, want StatusCurrent or StatusStale", st)
	}
}

// @s2 — rebind-only keeps sync_id stable and round-trips without divergence. The
// default (no --name) path leaves the topic key unchanged, so sync_id is
// identical before and after; and an ExportRecords -> ImportRecords -> re-export
// reproduces the rebound record byte-for-byte on its shareable fields.
func TestApplyPlan_RebindKeepsSyncIDStable_RoundTrips(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-orphan", "acme-ghost")

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/acme-orphan",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	beforeSync := getRecord(t, store, "skill/acme-orphan").SyncID

	if _, err := ApplyPlan(store, plan, clusters, t.TempDir()); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	afterSync := getRecord(t, store, "skill/acme-orphan").SyncID
	if afterSync != beforeSync {
		t.Errorf("sync_id changed on rebind-only path: before=%q after=%q", beforeSync, afterSync)
	}

	// Export round-trip into a fresh store reproduces the record without divergence.
	recs1, err := store.ExportRecords("alice")
	if err != nil {
		t.Fatalf("ExportRecords (source): %v", err)
	}
	store2 := newStore(t)
	if _, err := store2.ImportRecords(recs1); err != nil {
		t.Fatalf("ImportRecords: %v", err)
	}
	recs2, err := store2.ExportRecords("alice")
	if err != nil {
		t.Fatalf("ExportRecords (round-trip): %v", err)
	}

	c1, ok1 := chunkByTopic(recs1, "skill/acme-orphan")
	c2, ok2 := chunkByTopic(recs2, "skill/acme-orphan")
	if !ok1 || !ok2 {
		t.Fatalf("rebound record missing from export: source=%v roundtrip=%v", ok1, ok2)
	}
	if c1.SyncID != c2.SyncID || c1.TopicKey != c2.TopicKey || c1.Content != c2.Content ||
		c1.Type != c2.Type || c1.Revision != c2.Revision {
		t.Errorf("round-trip diverged:\n source=%+v\nroundtrip=%+v", c1, c2)
	}
	// And the round-tripped sync_id is still the original, stable one.
	if c2.SyncID != beforeSync {
		t.Errorf("round-tripped sync_id = %q, want stable %q", c2.SyncID, beforeSync)
	}
}

// chunkByTopic finds the exported chunk record for a topic key.
func chunkByTopic(recs []memory.ChunkRecord, topic string) (memory.ChunkRecord, bool) {
	for _, r := range recs {
		if r.TopicKey == topic {
			return r, true
		}
	}
	return memory.ChunkRecord{}, false
}

// @s3 — a second apply is a no-op (idempotent). After the corpus is reconciled
// once, running ApplyPlan again reports zero actions and makes no database or
// filesystem change: the record now binds to a live cluster, so it is no longer
// an orphan and there is nothing to rewrite.
func TestApplyPlan_SecondApplyIsNoOp(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-orphan", "acme-ghost")
	skillsDir := t.TempDir()

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/acme-orphan",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	first, err := ApplyPlan(store, plan, clusters, skillsDir)
	if err != nil {
		t.Fatalf("ApplyPlan (first): %v", err)
	}
	if len(first.Rebound) != 1 {
		t.Fatalf("first apply must rebind the orphan; got %d action(s): %+v", len(first.Rebound), first.Rebound)
	}

	afterFirstStore := snapshotStore(t, store)
	afterFirstFiles := snapshotFiles(t, skillsDir)

	second, err := ApplyPlan(store, plan, clusters, skillsDir)
	if err != nil {
		t.Fatalf("ApplyPlan (second): %v", err)
	}
	if len(second.Rebound) != 0 || len(second.Divergent) != 0 {
		t.Errorf("second apply not a no-op: rebound=%+v divergent=%+v", second.Rebound, second.Divergent)
	}
	if got := snapshotStore(t, store); !mapsEqual(afterFirstStore, got) {
		t.Errorf("second apply mutated the store:\n after1 = %v\n after2 = %v", afterFirstStore, got)
	}
	if got := snapshotFiles(t, skillsDir); !mapsEqual(afterFirstFiles, got) {
		t.Errorf("second apply mutated the skills dir:\n after1 = %v\n after2 = %v", afterFirstFiles, got)
	}
}

// mapsEqual compares two string->string maps.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// @s4 — an ambiguous orphan is left unchanged under bare --apply. Two comparable
// candidate clusters (neither uniquely clearing 0.5) mean AutoApplyTarget yields
// no target, so the orphan is not rewritten: its provenance label is untouched
// and it still classifies as [orphan].
func TestApplyPlan_AmbiguousOrphanLeftUnchanged(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-orphan", "acme-ghost")

	clusters := []crystallize.Cluster{
		clus("acme-checkout", "testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax"),
		clus("acme-shipping", "testing/shipping-flow", "gotcha/shipping-null", "decision/shipping-rate"),
	}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic: "skill/acme-orphan",
		Label: "acme-ghost",
		Candidates: []Candidate{
			{Label: "acme-checkout", Overlap: 0.6},
			{Label: "acme-shipping", Overlap: 0.6},
		},
	}}}

	res, err := ApplyPlan(store, plan, clusters, t.TempDir())
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if len(res.Rebound) != 0 {
		t.Errorf("ambiguous orphan was rebound under bare --apply: %+v", res.Rebound)
	}

	rec := getRecord(t, store, "skill/acme-orphan")
	if got := crystallize.ParseLabel(rec.Content); got != "acme-ghost" {
		t.Errorf("ambiguous orphan label changed to %q; want it left as %q", got, "acme-ghost")
	}
	if st := crystallize.RecordStatus(rec, byLabelMap(clusters)); st != crystallize.StatusOrphan {
		t.Errorf("ambiguous orphan status = %v, want StatusOrphan (left visibly orphaned)", st)
	}
}

// @s5 — a hand-written SKILL.md is preserved; only the record is updated. When
// the record's on-disk SKILL.md carries no crystallize marker (hand-written,
// MaterializeDecision == false), rebind updates the record's provenance but
// leaves the file bytes byte-for-byte intact and reports the divergence.
func TestApplyPlan_HandWrittenSkillPreserved(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-handwritten", "acme-ghost")

	// A hand-written SKILL.md (no crystallize marker) at the record's skill name.
	skillsDir := t.TempDir()
	skillPath := filepath.Join(skillsDir, "acme-handwritten", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	handBytes := []byte("---\nname: acme-handwritten\n---\n# Curated by a human. Do not clobber.\n")
	if err := os.WriteFile(skillPath, handBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/acme-handwritten",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	res, err := ApplyPlan(store, plan, clusters, skillsDir)
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	// The on-disk file bytes are unchanged.
	gotBytes, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(handBytes) {
		t.Errorf("hand-written SKILL.md was rewritten:\n before = %q\n after  = %q", handBytes, gotBytes)
	}

	// The record itself WAS rebound (provenance updated to the matched label).
	rec := getRecord(t, store, "skill/acme-handwritten")
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("record label = %q, want it rebound to %q even though the file was preserved", got, "acme-checkout")
	}

	// The divergence (record updated, file left intact) is reported.
	if !hasDivergence(res.Divergent, "skill/acme-handwritten") {
		t.Errorf("divergence not reported for the preserved hand-written skill: %+v", res.Divergent)
	}
}
