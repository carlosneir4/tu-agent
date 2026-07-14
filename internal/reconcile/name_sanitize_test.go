package reconcile

// RED-phase tests for feature `reconcile-name-sanitize` (@f0-1) — scenarios
// @s1..@s4 of
//   .tu-agent/tdd/f0-security-hardening-fixes-plan-version-es/features/reconcile-name-sanitize.feature
//
// CRITICAL path-traversal guard: ApplyOptions.Name flows unsanitized into
// "skill/"+opts.Name and filepath.Join(skillsDir, opts.Name) + os.Rename
// (internal/reconcile/rename.go). A value like "../../evil" moves the on-disk
// folder OUTSIDE .claude/skills. This locks the contract (spec.md item 1):
// ApplyPlanWithOptions must reject opts.Name that is ".", "..", contains "/",
// contains a backslash, or contains filepath.Separator — validated BEFORE any
// mutation (before the provenance Upsert), mirroring the existing
// destination-collision guard placed at the same point in ApplyPlanWithOptions
// and the single-path-segment predicate at cmd/tu-agent/memory.go:804. An
// empty Name is NOT a traversal attempt — it means "no rename" and stays
// valid (@s4, may already pass against existing code: that is fine, the RED
// signal for this feature is @s1-@s3).
//
// Reuses helpers from apply_test.go / rename_test.go (same package): newStore,
// seedOrphan, getRecord, materializeSkill, isDir. §9: generic acme-* fixtures.

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
)

// @s1 — a ".." segment is rejected before any mutation. ApplyOptions.Name set
// to "../evil" must fail with an error naming the rejected value, and neither
// the on-disk folder nor the record's provenance may be touched: the guard
// must run before the provenance Upsert (same position as the existing
// destination-collision guard).
func TestApplyPlanWithOptions_DotDotSegmentRejectedBeforeMutation(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "old", true) // crystallize-managed folder that must NOT move

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	before := getRecord(t, store, "skill/old")

	const traversal = "../evil"
	_, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: traversal, ToCluster: "acme-checkout"})
	if err == nil {
		t.Fatalf("ApplyPlanWithOptions must reject a Name containing %q", traversal)
	}
	if !strings.Contains(err.Error(), traversal) {
		t.Errorf("error %q does not name the rejected value %q", err.Error(), traversal)
	}
	if !strings.Contains(err.Error(), "reconcile.ApplyPlanWithOptions:") {
		t.Errorf("error %q missing the conventional %q wrap context", err.Error(), "reconcile.ApplyPlanWithOptions:")
	}

	// No folder is moved or removed under skillsDir: the source folder is still
	// there, untouched, and no folder was created at any traversal-derived path.
	if !isDir(filepath.Join(skillsDir, "old")) {
		t.Errorf("source folder old/ disappeared despite the rejected Name")
	}
	if isDir(filepath.Join(filepath.Dir(skillsDir), "evil")) {
		t.Errorf("traversal folder was created one level above skillsDir")
	}

	// The record's provenance is unchanged: no Upsert (content/revision) fired.
	after := getRecord(t, store, "skill/old")
	if after.Content != before.Content {
		t.Errorf("record content mutated despite the rejected Name:\n before = %q\n after  = %q", before.Content, after.Content)
	}
	if after.Revision != before.Revision {
		t.Errorf("record revision bumped despite the rejected Name: before=%d after=%d", before.Revision, after.Revision)
	}
	if after.SyncID != before.SyncID {
		t.Errorf("record sync_id changed despite the rejected Name: before=%q after=%q", before.SyncID, after.SyncID)
	}
}

// @s2 — a "/" inside Name is rejected as a multi-segment path. The record must
// be left completely untouched (no rebind, no rename).
func TestApplyPlanWithOptions_SlashInsideNameRejected(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")
	before := getRecord(t, store, "skill/old")

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	_, err := ApplyPlanWithOptions(store, plan, clusters, t.TempDir(),
		ApplyOptions{Name: "sub/evil", ToCluster: "acme-checkout"})
	if err == nil {
		t.Fatalf("ApplyPlanWithOptions must reject a Name containing %q", "sub/evil")
	}

	after := getRecord(t, store, "skill/old")
	if after.Content != before.Content || after.Revision != before.Revision {
		t.Errorf("record was touched despite the rejected multi-segment Name:\n before = %+v\n after  = %+v", before, after)
	}
}

// @s3 — a bare "." or ".." Name is rejected; no rename is attempted, and the
// record's provenance stays untouched.
func TestApplyPlanWithOptions_BareDotOrDotDotRejected(t *testing.T) {
	for _, name := range []string{".", ".."} {
		t.Run(name, func(t *testing.T) {
			store := newStore(t)
			seedOrphan(t, store, "old", "acme-ghost")
			before := getRecord(t, store, "skill/old")

			skillsDir := t.TempDir()
			materializeSkill(t, skillsDir, "old", true)

			target := clus("acme-checkout",
				"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
			clusters := []crystallize.Cluster{target}
			plan := Plan{Orphans: []OrphanPlan{{
				Topic:      "skill/old",
				Label:      "acme-ghost",
				Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
			}}}

			_, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
				ApplyOptions{Name: name, ToCluster: "acme-checkout"})
			if err == nil {
				t.Fatalf("ApplyPlanWithOptions must reject bare Name %q", name)
			}
			// The rejection must come from the DEDICATED Name-sanitize guard (which
			// names the rejected value, quoted), not merely happen to error out via
			// the unrelated destination-collision guard (which today incidentally
			// fires for "." / ".." because filepath.Join(skillsDir, ".") == skillsDir
			// and filepath.Join(skillsDir, "..") == its parent, both of which exist).
			if !strings.Contains(err.Error(), strconv.Quote(name)) {
				t.Errorf("error %q does not name the rejected bare value %q", err.Error(), name)
			}

			// No rename attempted: the source folder is untouched, still at old/.
			if !isDir(filepath.Join(skillsDir, "old")) {
				t.Errorf("source folder old/ disappeared despite the rejected bare Name %q", name)
			}

			after := getRecord(t, store, "skill/old")
			if after.Content != before.Content || after.Revision != before.Revision {
				t.Errorf("record was touched despite the rejected bare Name %q:\n before = %+v\n after  = %+v", name, before, after)
			}
		})
	}
}

// @s4 — an empty Name is still valid: it means "no rename requested", and the
// existing rebind-only behavior applies unchanged (no error for the Name
// field). This scenario may already pass against the current code — that is
// fine; the RED signal for this feature is carried by @s1-@s3 above.
func TestApplyPlanWithOptions_EmptyNameStillValid(t *testing.T) {
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

	res, err := ApplyPlanWithOptions(store, plan, clusters, t.TempDir(), ApplyOptions{Name: ""})
	if err != nil {
		t.Fatalf("ApplyPlanWithOptions must accept an empty Name (no rename requested): %v", err)
	}

	// Rebind-only behavior applies: the orphan is reported in Rebound, not Renamed.
	if len(res.Rebound) != 1 {
		t.Fatalf("Rebound = %d action(s), want exactly 1 (empty Name is rebind-only): %+v", len(res.Rebound), res.Rebound)
	}
	if len(res.Renamed) != 0 {
		t.Errorf("Renamed = %+v, want none for an empty Name", res.Renamed)
	}
	rec := getRecord(t, store, "skill/acme-orphan")
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("rebound label = %q, want %q", got, "acme-checkout")
	}
}
