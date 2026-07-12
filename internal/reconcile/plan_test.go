package reconcile

// RED-phase tests for feature `reconcile-dryrun-suggest` (leg 5) — scenarios
// @s1..@s4 of
//   .tu-agent/tdd/crystallize-community-detection-clusteri/features/reconcile-dryrun-suggest.feature
//
// These reference the deterministic reconcile CORE the implementer will ADD to
// this (currently source-less) package, so they FAIL TO COMPILE until it exists
// — the correct RED, and the failure is ISOLATED to internal/reconcile: no
// other package's test binary is blocked (sibling legs live in
// internal/crystallize, internal/memory, and cmd/tu-agent, which all still
// build). The core is the shared dry-run planner both `memory reconcile` (CLI)
// and `mem_reconcile` (MCP) will drive (§10 parity).
//
// Production API this file expects (to be ADDED to internal/reconcile):
//
//   type Candidate struct { Label string; Overlap float64 }
//   type OrphanPlan struct { Topic, Label string; Candidates []Candidate }
//   type Folder     struct { Name string; Marked bool }
//   type Plan       struct { Orphans []OrphanPlan }
//
//   // Suggest ranks live clusters as candidate targets for an orphan by
//   // member-overlap Jaccard of their topic-key sets, ordered
//   // (Overlap desc, Label asc). Pure and deterministic.
//   func Suggest(orphanMembers []memory.Observation, clusters []crystallize.Cluster) []Candidate
//
//   // AutoApplyTarget encodes the locked ambiguity rule (D5/decision 2): a
//   // remap is auto-applyable ONLY when exactly one candidate clears
//   // overlap >= 0.5; otherwise ("", false) and the orphan stays visibly
//   // orphaned. The dry-run report uses this to decide whether to present a
//   // target at all.
//   func AutoApplyTarget(candidates []Candidate) (string, bool)
//
//   // PlanFrom builds the dry-run reconcile plan from an in-memory corpus and
//   // the materialized skill folders. Pure, order-invariant in both slices,
//   // mutates neither.
//   func PlanFrom(obs []memory.Observation, folders []Folder, minSize int) Plan
//
//   // DryRun is the store/disk adapter over PlanFrom: it reads the live
//   // observations and scans skillsDir, then returns the plan. It WRITES
//   // NOTHING (dry-run is the default; --apply is a later leg).
//   func DryRun(store *memory.Store, skillsDir string, minSize int) (Plan, error)
//
// Fixtures are generic and fictional (acme-*) per repo §9.

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

// mem builds a bare observation carrying only a topic key — enough for
// member-overlap scoring, which keys on TopicKey.
func mem(topic string) memory.Observation { return memory.Observation{TopicKey: topic} }

// clus builds a live cluster with the given label and member topic keys.
func clus(label string, topics ...string) crystallize.Cluster {
	members := make([]memory.Observation, len(topics))
	for i, t := range topics {
		members[i] = mem(t)
	}
	return crystallize.Cluster{Label: label, Members: members, Size: len(members)}
}

// orphanRecord builds a skill record (Type "skill", topic skill/<name>) whose
// provenance label matches no live cluster, so it reads as an orphan.
func orphanRecord(name, label string, members []memory.Observation) memory.Observation {
	content := "---\nname: " + name + "\n---\n" +
		crystallize.ProvenanceLine(label, members) + "\nbody for " + label + "\n"
	return memory.Observation{Type: "skill", TopicKey: "skill/" + name, Content: content, Revision: 1}
}

func labelsOf(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Label
	}
	return out
}

func approxEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}

// @s2 — candidates are ranked by member-overlap Jaccard, deterministically.
// An orphan whose members overlap several live clusters by different amounts
// yields candidates ordered by (overlap descending, label ascending), and the
// ranking is byte-identical across repeated calls.
func TestSuggest_RanksByOverlapThenLabel_Deterministic(t *testing.T) {
	orphanMembers := []memory.Observation{mem("a1"), mem("a2"), mem("a3"), mem("a4")}

	// high:  ∩={a1,a2,a3}=3, ∪={a1,a2,a3,a4,x1}=5 → 0.6
	// mid-a: ∩={a1,a2}=2,    ∪={a1,a2,a3,a4,p1,p2}=6 → 0.333…
	// mid-z: ∩={a1,a2}=2,    ∪={a1,a2,a3,a4,q1,q2}=6 → 0.333… (ties mid-a)
	clusters := []crystallize.Cluster{
		clus("acme-mid-z", "a1", "a2", "q1", "q2"),
		clus("acme-high", "a1", "a2", "a3", "x1"),
		clus("acme-mid-a", "a1", "a2", "p1", "p2"),
	}

	got := Suggest(orphanMembers, clusters)

	wantOrder := []string{"acme-high", "acme-mid-a", "acme-mid-z"}
	if gotOrder := labelsOf(got); !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("candidate order = %v, want %v (overlap desc, then label asc)", gotOrder, wantOrder)
	}

	// Overlap values are the Jaccard scores that drove the ranking.
	if !approxEqual(got[0].Overlap, 0.6) {
		t.Errorf("top candidate overlap = %v, want 0.6", got[0].Overlap)
	}
	if got[0].Overlap <= got[1].Overlap {
		t.Errorf("overlaps not strictly descending at the top: %v then %v", got[0].Overlap, got[1].Overlap)
	}
	if !approxEqual(got[1].Overlap, got[2].Overlap) {
		t.Errorf("tied candidates have unequal overlap: %v vs %v", got[1].Overlap, got[2].Overlap)
	}

	// Determinism: a second call yields an identical ranking.
	if again := Suggest(orphanMembers, clusters); !reflect.DeepEqual(again, got) {
		t.Errorf("ranking not stable across runs:\n first = %+v\nsecond = %+v", got, again)
	}
}

// @s4 — an ambiguous orphan is reported, not silently mapped. When two live
// clusters sit at comparable overlap (both clearing the 0.5 auto-apply bar), the
// dry-run surfaces both as competing candidates and presents NO single target as
// an auto-apply decision. The unambiguous single-candidate case, by contrast,
// DOES yield a target — proving the rule is the ambiguity, not a blanket refusal.
func TestAutoApply_AmbiguousReportedNotMapped(t *testing.T) {
	orphanMembers := []memory.Observation{mem("a1"), mem("a2"), mem("a3"), mem("a4")}

	// Two comparable candidates: each ∩=3, ∪=5 → 0.6, both >= 0.5.
	ambiguous := []crystallize.Cluster{
		clus("acme-beta", "a1", "a2", "a3", "z2"),
		clus("acme-alpha", "a1", "a2", "a3", "z1"),
	}
	cands := Suggest(orphanMembers, ambiguous)

	// Competing candidates are reported (the orphan stays visibly orphaned WITH
	// its rivals), not collapsed to one.
	if len(cands) < 2 {
		t.Fatalf("ambiguous orphan: got %d candidate(s), want the competing pair reported: %+v", len(cands), cands)
	}
	gotLabels := labelsOf(cands)
	sort.Strings(gotLabels)
	if !reflect.DeepEqual(gotLabels, []string{"acme-alpha", "acme-beta"}) {
		t.Errorf("competing candidates = %v, want both acme-alpha and acme-beta surfaced", gotLabels)
	}

	// No single target is presented as an auto-apply decision.
	if target, ok := AutoApplyTarget(cands); ok {
		t.Errorf("ambiguous orphan auto-mapped to %q; must stay orphaned (ok=%v)", target, ok)
	}

	// Boundary check: the UNAMBIGUOUS case (exactly one candidate >= 0.5) does
	// present a target — so the refusal above is specifically about ambiguity.
	solo := []Candidate{{Label: "acme-solo", Overlap: 0.7}}
	if target, ok := AutoApplyTarget(solo); !ok || target != "acme-solo" {
		t.Errorf("single high-overlap candidate: AutoApplyTarget = (%q, %v), want (\"acme-solo\", true)", target, ok)
	}
}

// @s3 — the plan is deterministic under input reordering. The same corpus and
// the same folders presented in a shuffled order produce a byte-identical plan:
// orphan order, suggestion ranking, and folder actions all match the unshuffled
// run. PlanFrom is tested directly (not via the store) because the store's List
// order is not itself canonical — the planner must impose the order.
func TestPlanFrom_DeterministicUnderReordering(t *testing.T) {
	obs := []memory.Observation{
		{TopicKey: "testing/checkout-flow", Type: "testing", Content: "checkout order total"},
		{TopicKey: "gotcha/checkout-null-cart", Type: "gotcha", Content: "checkout cart empty panic"},
		{TopicKey: "decision/checkout-tax", Type: "decision", Content: "checkout tax per region"},
		orphanRecord("acme-orphan-one", "acme-ghost-one", []memory.Observation{mem("reference/acme-ghost-one")}),
		orphanRecord("acme-orphan-two", "acme-ghost-two", []memory.Observation{mem("reference/acme-ghost-two")}),
	}
	folders := []Folder{
		{Name: "checkout", Marked: true},
		{Name: "acme-orphan-one", Marked: true},
		{Name: "acme-orphan-two", Marked: true},
		{Name: "hand-written-skill", Marked: false},
	}

	base := PlanFrom(obs, folders, 3)

	// A non-trivial plan: both orphans must be present so the ordering assertion
	// is meaningful.
	if len(base.Orphans) != 2 {
		t.Fatalf("PlanFrom found %d orphan(s), want 2: %+v", len(base.Orphans), base.Orphans)
	}

	// Reverse both inputs and re-plan; the plan must be identical.
	shObs := make([]memory.Observation, len(obs))
	for i := range obs {
		shObs[i] = obs[len(obs)-1-i]
	}
	shFolders := make([]Folder, len(folders))
	for i := range folders {
		shFolders[i] = folders[len(folders)-1-i]
	}
	shuffled := PlanFrom(shObs, shFolders, 3)

	if !reflect.DeepEqual(base, shuffled) {
		t.Errorf("plan not order-invariant:\nunshuffled = %+v\n  shuffled = %+v", base, shuffled)
	}
}

// @s1 — dry-run mutates nothing. Against a real store and a real skills folder,
// DryRun (the default, no --apply) must leave every observation row and its
// sync_id, and every .claude/skills/*/SKILL.md byte, identical before and after.
func TestDryRun_MutatesNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// A live "checkout" cluster (3 cohesive notes).
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := store.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	// An orphan skill record bound to a label no live cluster carries.
	orphan := orphanRecord("acme-orphan", "acme-ghost", []memory.Observation{mem("reference/acme-ghost")})
	if _, err := store.Upsert(orphan.TopicKey, orphan.Content, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}

	// A materialized (marked) skill folder on disk.
	skillsDir := t.TempDir()
	ghostSkill := filepath.Join(skillsDir, "acme-ghost", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(ghostSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	skillBytes := []byte(orphan.Content)
	if err := os.WriteFile(ghostSkill, skillBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshotStore(t, store)
	beforeFiles := snapshotFiles(t, skillsDir)

	plan, err := DryRun(store, skillsDir, 3)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	// Prove the dry-run actually did work (so "mutates nothing" isn't vacuous).
	if len(plan.Orphans) == 0 {
		t.Fatalf("DryRun reported no orphans; expected the acme-ghost record to be one")
	}

	if after := snapshotStore(t, store); !reflect.DeepEqual(before, after) {
		t.Errorf("DryRun mutated observation rows / sync_ids:\nbefore = %v\n after = %v", before, after)
	}
	if afterFiles := snapshotFiles(t, skillsDir); !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Errorf("DryRun mutated .claude/skills bytes:\nbefore = %v\n after = %v", beforeFiles, afterFiles)
	}
}

// snapshotStore captures every live observation by id → topic|sync_id|rev|content,
// so any renamed key, recomputed sync_id, bumped revision, or edited body shows
// up as a diff.
func snapshotStore(t *testing.T, s *memory.Store) map[string]string {
	t.Helper()
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(obs))
	for _, o := range obs {
		out[o.ID] = o.TopicKey + "|" + o.SyncID + "|" + itoa(o.Revision) + "|" + o.Content
	}
	return out
}

// snapshotFiles captures the bytes of every file under dir keyed by relative path.
func snapshotFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
