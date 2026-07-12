package reconcile

// RED-phase tests for feature B1 — extend the reconcile apply-core RESULT model
// so an apply run can be SUMMARIZED, plus a deterministic renderer that is a
// sibling of RenderPlan.
//
// The apply core today reports only Rebound + Divergent. It does NOT:
//   - distinguish a topic RENAME (--name) from a pure rebind,
//   - report crystallize-managed folders that reconcileFolders REMOVED,
//   - report orphans it left UNTOUCHED (ambiguous / no resolvable target).
// B1 adds those three, plus RenderApplyResult. These tests fail to COMPILE until
// the NEW symbols exist (RenameAction, ApplyResult.Renamed/Removed/Skipped,
// RenderApplyResult) — the correct RED, ISOLATED to internal/reconcile.
//
// ---------------------------------------------------------------------------
// EXTENDED API this file pins (to be ADDED to internal/reconcile) — DECISIONS
// for the implementer:
//
//   // RenameAction is one topic RENAME an apply run performed (the --name path):
//   // the record's old topic key, its new topic key, and the live cluster label
//   // it was re-pointed to. A rename is reported HERE, NOT also in Rebound (a
//   // renamed record must NOT be double-counted as a plain rebound).
//   type RenameAction struct { OldTopic, NewTopic, Label string }
//
//   // ApplyResult reports what an apply run changed.
//   type ApplyResult struct {
//       Rebound   []ReboundAction // pure rebinds (label re-point, sync_id stable)
//       Renamed   []RenameAction  // --name renames (topic + folder moved)
//       Divergent []Divergence    // record moved, hand-written SKILL.md left intact
//       Removed   []string        // crystallize-managed skill FOLDER NAMES that
//                                 // reconcileFolders deleted (no live record)
//       Skipped   []string        // plan-orphan TOPIC KEYS left untouched because
//                                 // resolveTarget found no target (ambiguous / none)
//   }
//
//   // RenderApplyResult renders an ApplyResult to deterministic, byte-stable
//   // text. Pure and a sibling of RenderPlan. Ordering within each category is
//   // the slice order as given (the core sorts upstream); the renderer does NOT
//   // re-sort. Chosen format (PINNED below):
//   //
//   //   Applied reconcile: <R> rebound, <N> renamed, <D> divergence
//   //   - rebound  <topic>  (<oldLabel> -> <newLabel>)
//   //   - renamed  <oldTopic> -> <newTopic>  (label: <label>)
//   //   - diverge  <topic>  (hand-written SKILL.md left intact)
//   //   - removed  skill/<name>  (folder deleted)
//   //   <S> orphan left untouched (ambiguous): <topic>[, <topic>...]
//   //
//   // Header lists ONLY rebound/renamed/divergence counts (removed & skipped are
//   // surfaced as their own lines, not header counts). Line tags are all 7 chars
//   // ("rebound"/"renamed"/"diverge"/"removed") so spacing is a fixed
//   // "- <tag>  <subject>  (<detail>)" with no padding. Divergence renders its
//   // Topic (not Path) with fixed detail text. Removed renders the folder name
//   // as skill/<name>. The trailing "untouched" line is emitted ONLY when Skipped
//   // is non-empty. An EMPTY ApplyResult renders a single "nothing to apply" line.
//   func RenderApplyResult(res ApplyResult) string
// ---------------------------------------------------------------------------
//
// Reuses helpers from plan_test.go / apply_test.go / rename_test.go: newStore,
// seedOrphan, getRecord, byLabelMap, materializeSkill, lookupRecord, isDir, clus,
// crystallize helpers. §9: generic acme-* fixtures.

import (
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
)

// containsStr reports whether xs contains s.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// findRename returns the RenameAction whose OldTopic matches, if any.
func findRename(rs []RenameAction, oldTopic string) (RenameAction, bool) {
	for _, r := range rs {
		if r.OldTopic == oldTopic {
			return r, true
		}
	}
	return RenameAction{}, false
}

// B1-1 — a --name RENAME is reported in Renamed, NOT in Rebound. An orphan
// skill/old with a crystallize-managed folder, applied with
// ApplyOptions{Name:"new", ToCluster:<label>}, produces exactly one RenameAction
// {skill/old -> skill/new, label:<matched>} and ZERO Rebound actions: a rename
// must not also be double-counted as a plain rebind. RED against current code,
// which appends a Rebound for the renamed record.
func TestApplyResult_RenameReportedNotRebound(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "old", true) // marked folder, eligible to rename

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	res, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"})
	if err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	// A rename is a RenameAction, not a ReboundAction.
	if len(res.Renamed) != 1 {
		t.Fatalf("Renamed = %d action(s), want exactly 1: %+v", len(res.Renamed), res.Renamed)
	}
	got, ok := findRename(res.Renamed, "skill/old")
	if !ok {
		t.Fatalf("no RenameAction for skill/old: %+v", res.Renamed)
	}
	if got.NewTopic != "skill/new" || got.Label != "acme-checkout" {
		t.Errorf("Renamed[0] = %+v, want {OldTopic:skill/old NewTopic:skill/new Label:acme-checkout}", got)
	}
	// The rename is NOT also counted as a plain rebound.
	if len(res.Rebound) != 0 {
		t.Errorf("Rebound = %d action(s), want 0 (a rename must not double-count as a rebound): %+v",
			len(res.Rebound), res.Rebound)
	}

	// Sanity: the record really did move (so this isn't a vacuous no-op).
	if _, ok := lookupRecord(t, store, "skill/new"); !ok {
		t.Errorf("record skill/new not present; the rename did not happen")
	}
}

// B1-1b — a PURE --apply (no --name) is a plain rebind, so it populates Rebound
// and leaves Renamed empty. This is the complement to B1-1: the two categories
// are mutually exclusive per apply action.
func TestApplyResult_PlainRebindNotCountedAsRename(t *testing.T) {
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
	if len(res.Rebound) != 1 {
		t.Errorf("Rebound = %d, want 1 for a plain rebind: %+v", len(res.Rebound), res.Rebound)
	}
	if len(res.Renamed) != 0 {
		t.Errorf("Renamed = %d, want 0 for a plain rebind (no --name): %+v", len(res.Renamed), res.Renamed)
	}
}

// B1-2 — removed crystallize-managed folders are reported in Removed; a
// hand-written folder with no record is NOT reported and stays on disk. A marked
// folder "ghost" backing no live record is deleted AND its name appears in
// res.Removed; a hand-written folder "acme-manual" (no marker, no record) is left
// intact AND is NOT in res.Removed.
func TestApplyResult_RemovedFoldersReported(t *testing.T) {
	store := newStore(t)

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "ghost", true)        // marked, no record -> removed + reported
	materializeSkill(t, skillsDir, "acme-manual", false) // hand-written, no record -> kept + not reported

	res, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{})
	if err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	// The marked orphan folder is both deleted AND reported.
	if isDir(filepath.Join(skillsDir, "ghost")) {
		t.Errorf("marked folder ghost (no live record) was not removed on disk")
	}
	if !containsStr(res.Removed, "ghost") {
		t.Errorf("Removed = %v, want it to contain %q", res.Removed, "ghost")
	}

	// The hand-written folder is neither deleted nor reported.
	if !isDir(filepath.Join(skillsDir, "acme-manual")) {
		t.Errorf("hand-written folder acme-manual was removed; it must be preserved")
	}
	if containsStr(res.Removed, "acme-manual") {
		t.Errorf("Removed = %v, must NOT contain the hand-written folder %q", res.Removed, "acme-manual")
	}
}

// B1-3 — an orphan left UNTOUCHED (ambiguous, no resolvable target) is reported
// in Skipped and is NOT mutated. Under bare --apply, two comparable candidates
// mean AutoApplyTarget yields no target, so the orphan is skipped: its topic
// appears in res.Skipped, it is not rebound or renamed, and it still classifies
// as StatusOrphan in the store.
func TestApplyResult_AmbiguousOrphanReportedSkipped(t *testing.T) {
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

	// The skipped orphan is reported by topic.
	if !containsStr(res.Skipped, "skill/acme-orphan") {
		t.Errorf("Skipped = %v, want it to contain the untouched orphan %q", res.Skipped, "skill/acme-orphan")
	}
	// It was NOT acted on either way.
	if len(res.Rebound) != 0 || len(res.Renamed) != 0 {
		t.Errorf("ambiguous orphan was acted on: rebound=%+v renamed=%+v", res.Rebound, res.Renamed)
	}

	// It is genuinely untouched in the store: label unchanged, still orphaned.
	rec := getRecord(t, store, "skill/acme-orphan")
	if got := crystallize.ParseLabel(rec.Content); got != "acme-ghost" {
		t.Errorf("skipped orphan label changed to %q; want it left as %q", got, "acme-ghost")
	}
	if st := crystallize.RecordStatus(rec, byLabelMap(clusters)); st != crystallize.StatusOrphan {
		t.Errorf("skipped orphan status = %v, want StatusOrphan (left visibly orphaned)", st)
	}
}

// B1-4 — RenderApplyResult is a deterministic, byte-stable sibling of RenderPlan.
// Built DIRECTLY from structs (not from a core run) so the assertion pins the
// exact text independent of the apply mechanics. Mix: 2 rebound, 1 renamed, 1
// divergence, 1 removed, 1 skipped. Ordering within each category is the slice
// order as given (the renderer does NOT re-sort). See the API-DECISION header for
// the format rationale.
func TestRenderApplyResult_MixedResult(t *testing.T) {
	res := ApplyResult{
		Rebound: []ReboundAction{
			{Topic: "skill/checkout", OldLabel: "checkout-flow", NewLabel: "payment-checkout"},
			{Topic: "skill/auth", OldLabel: "auth-legacy", NewLabel: "auth"},
		},
		Renamed: []RenameAction{
			{OldTopic: "skill/old", NewTopic: "skill/new", Label: "cart"},
		},
		Divergent: []Divergence{
			{Topic: "skill/manual", Path: "/tmp/acme-manual/SKILL.md"},
		},
		Removed: []string{"ghost"},
		Skipped: []string{"skill/misc"},
	}

	want := "Applied reconcile: 2 rebound, 1 renamed, 1 divergence\n" +
		"- rebound  skill/checkout  (checkout-flow -> payment-checkout)\n" +
		"- rebound  skill/auth  (auth-legacy -> auth)\n" +
		"- renamed  skill/old -> skill/new  (label: cart)\n" +
		"- diverge  skill/manual  (hand-written SKILL.md left intact)\n" +
		"- removed  skill/ghost  (folder deleted)\n" +
		"1 orphan left untouched (ambiguous): skill/misc\n"

	if got := RenderApplyResult(res); got != want {
		t.Errorf("mixed apply-result render mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// B1-4b — an EMPTY ApplyResult (nothing changed) renders one clear line.
func TestRenderApplyResult_EmptyResult(t *testing.T) {
	want := "Applied reconcile: nothing to apply; memory already reconciled.\n"
	if got := RenderApplyResult(ApplyResult{}); got != want {
		t.Errorf("empty apply-result render mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
