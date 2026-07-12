package reconcile

// RED-phase tests for feature `reconcile-rename-folders` (leg 5, feature 3 of 4)
// — scenarios @s1..@s5 of
//   .tu-agent/tdd/crystallize-community-detection-clusteri/features/reconcile-rename-folders.feature
//
// This is the OPT-IN rename path (`--name` / `--to-cluster`) plus the D8
// folder/record reconciliation. Feature 2 delivered the DEFAULT rebind-only
// `ApplyPlan(store, plan, clusters, skillsDir)` (chunk-clean, sync_id-stable).
// Feature 3 adds:
//   - `--name new`     : rename the record skill/<old> -> skill/<new> via
//                        memory.Store.Retopic (in-place UPDATE that recomputes
//                        sync_id and bumps revision) AND rename the on-disk
//                        marked folder .claude/skills/<old>/ -> <new>/, minting a
//                        NEW sync_id == computeSyncID(scope, "skill/<new>").
//   - `--to-cluster L` : force the re-point target to the named live cluster,
//                        overriding AutoApplyTarget even for an ambiguous orphan
//                        that bare `--apply` would leave untouched.
//   - folder reconcile : a marked (crystallize-managed) folder that maps to no
//                        live record is REMOVED; a hand-written folder (no
//                        crystallize marker) is left byte-for-byte untouched.
//
// ---------------------------------------------------------------------------
// API DECISION (for the implementer) — read this before coding.
//
// The existing `ApplyPlan(store, plan, clusters, skillsDir) (ApplyResult, error)`
// signature is load-bearing: feature 2's tests (apply_test.go) drive it directly
// and MUST stay green. So do NOT change its signature. Instead add a SIBLING that
// carries the opt-in flags, and make ApplyPlan delegate to it with zero options:
//
//   // ApplyOptions carries the opt-in rename-path flags. The zero value
//   // (both fields "") reproduces the default rebind-only ApplyPlan behaviour.
//   type ApplyOptions struct {
//       Name      string // --name: rename skill/<old> -> skill/<Name> (+folder), mints a new sync_id
//       ToCluster string // --to-cluster: force the re-point target to this live cluster's label
//   }
//
//   // ApplyPlanWithOptions is the rename-aware apply core. With a zero
//   // ApplyOptions it is exactly ApplyPlan (rebind-only). Otherwise, for each
//   // orphan in the plan:
//   //   * if opts.ToCluster != "" and names a live cluster, that label is the
//   //     forced target (it overrides AutoApplyTarget, even when the orphan is
//   //     ambiguous);
//   //   * the record's provenance is rewritten to label=<target> with the
//   //     source-hash recomputed against the matched cluster's members;
//   //   * if opts.Name != "", the record is renamed skill/<old> -> skill/<Name>
//   //     via store.Retopic(oldKey, "skill/"+Name, "project", "") — which mints a
//   //     new sync_id and bumps revision — and the on-disk MARKED folder
//   //     skillsDir/<old>/ is renamed (or re-materialized) to skillsDir/<Name>/;
//   //     a missing source folder is NOT an error.
//   // Independently of the orphan loop it reconciles folders under skillsDir:
//   // a MARKED folder whose skill name matches no live record is removed; a
//   // hand-written folder (no crystallize marker) is always preserved.
//   // Deterministic and idempotent.
//   func ApplyPlanWithOptions(store *memory.Store, plan Plan, clusters []crystallize.Cluster, skillsDir string, opts ApplyOptions) (ApplyResult, error)
//
//   // ApplyPlan then becomes:
//   //   return ApplyPlanWithOptions(store, plan, clusters, skillsDir, ApplyOptions{})
//
// These two NEW symbols (ApplyOptions, ApplyPlanWithOptions) are the ONLY
// undefined references below, so the RED failure is a clean compile error
// ISOLATED to internal/reconcile: sibling legs in internal/crystallize,
// internal/memory, and cmd/tu-agent still build, and feature 1/2's reconcile
// tests reference only already-landed symbols. All effects are observed through
// the real *memory.Store and the temp skills dir (authoritative), so the tests
// do not over-constrain the internal mechanics (Retopic + Upsert ordering, folder
// rename vs re-materialize).
// ---------------------------------------------------------------------------
//
// Depends on already-landed blocks (do NOT re-implement): memory.Store.Retopic,
// memory.Store.ExportRecords/ImportRecords, crystallize.Marker/ParseLabel/
// ParseSourceHash/SourceHash/ProvenanceLine/RecordStatus/MaterializeDecision.
// Reuses helpers from plan_test.go / apply_test.go: mem, clus, newStore,
// seedOrphan, getRecord, byLabelMap. §9: generic acme-* fixtures.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

// syncIDFor reproduces computeSyncID(scope="project", topic) — the deterministic
// sync_id memory.Store mints for a project-scoped record — by upserting the topic
// into a throwaway store and reading the value back. computeSyncID is unexported
// in package memory, so this is the observable way to pin the expected sync_id.
func syncIDFor(t *testing.T, topic string) string {
	t.Helper()
	s := newStore(t)
	o, err := s.Upsert(topic, "probe", memory.UpsertOpts{Type: "skill"})
	if err != nil {
		t.Fatal(err)
	}
	return o.SyncID
}

// materializeSkill writes skillsDir/<name>/SKILL.md. When marked, the body
// carries the crystallize marker (a crystallize-managed folder, safe to rewrite/
// rename); otherwise it is a hand-written skill with no marker.
func materializeSkill(t *testing.T, skillsDir, name string, marked bool) string {
	t.Helper()
	body := "---\nname: " + name + "\n---\n# hand-written; no marker\n"
	if marked {
		body = "---\nname: " + name + "\n---\n" +
			crystallize.ProvenanceLine(name, []memory.Observation{mem("reference/" + name)}) + "\nbody\n"
	}
	path := filepath.Join(skillsDir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedSkillRecord upserts a bare live skill record at skill/<name> (no orphan
// framing) so a folder of the same name maps to a live record.
func seedSkillRecord(t *testing.T, s *memory.Store, name string) {
	t.Helper()
	content := "---\nname: " + name + "\n---\nbody for " + name + "\n"
	if _, err := s.Upsert("skill/"+name, content, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}
}

// lookupRecord is the non-fatal counterpart of getRecord: it reports whether a
// topic key is present, used to assert both presence AND deliberate absence.
func lookupRecord(t *testing.T, s *memory.Store, topic string) (memory.Observation, bool) {
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

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// @s1 — `--name new --to-cluster <matched-label>` renames the record + folder and
// mints a new sync_id. Retopic moves skill/old -> skill/new and the on-disk
// marked folder old/ -> new/; the new sync_id equals computeSyncID(scope,
// "skill/new"); the revision is bumped; and the provenance label= is the matched
// cluster's label with a source-hash recomputed against that cluster's members.
func TestApplyRename_NameAndToClusterRenamesRecordAndFolder(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "old", true) // crystallize-managed folder, eligible to rename

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	before := getRecord(t, store, "skill/old")

	if _, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"}); err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	// The record moved: skill/old is gone, skill/new is present.
	if _, ok := lookupRecord(t, store, "skill/old"); ok {
		t.Errorf("skill/old still present after rename; want it moved to skill/new")
	}
	rec, ok := lookupRecord(t, store, "skill/new")
	if !ok {
		t.Fatalf("renamed record skill/new not found in store")
	}

	// New sync_id equals computeSyncID(scope, "skill/new") and differs from the old.
	wantSync := syncIDFor(t, "skill/new")
	if rec.SyncID != wantSync {
		t.Errorf("renamed sync_id = %q, want computeSyncID(project, skill/new) = %q", rec.SyncID, wantSync)
	}
	if rec.SyncID == before.SyncID {
		t.Errorf("sync_id was not minted anew on the --name path: still %q", rec.SyncID)
	}
	// Revision is bumped past the seeded revision.
	if rec.Revision <= before.Revision {
		t.Errorf("revision not bumped: before=%d after=%d", before.Revision, rec.Revision)
	}

	// Provenance label= is the matched cluster's label with a recomputed source-hash.
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("renamed label = %q, want %q", got, "acme-checkout")
	}
	wantHash := crystallize.SourceHash(target.Members)
	if got := crystallize.ParseSourceHash(rec.Content); got != wantHash {
		t.Errorf("renamed source-hash = %q, want SourceHash(matched members) = %q", got, wantHash)
	}

	// The folder moved: old/ is gone, new/ exists on disk.
	if isDir(filepath.Join(skillsDir, "old")) {
		t.Errorf("old skill folder still present after rename")
	}
	if !isDir(filepath.Join(skillsDir, "new")) {
		t.Errorf("renamed skill folder new/ not present on disk")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "new", "SKILL.md")); err != nil {
		t.Errorf("renamed folder has no SKILL.md: %v", err)
	}
}

// @s2 — `--to-cluster <label>` overrides the suggested target for an ambiguous
// orphan. Two comparable candidates mean bare --apply (AutoApplyTarget) yields no
// target and leaves the orphan untouched; naming the target explicitly rebinds it
// to that named cluster. No --name, so this is rebind-only (sync_id stable).
func TestApplyRename_ToClusterOverridesAmbiguous(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "acme-orphan", "acme-ghost")

	checkout := clus("acme-checkout", "testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	shipping := clus("acme-shipping", "testing/shipping-flow", "gotcha/shipping-null", "decision/shipping-rate")
	clusters := []crystallize.Cluster{checkout, shipping}
	// Ambiguous: both candidates clear the 0.5 floor, so AutoApplyTarget refuses.
	plan := Plan{Orphans: []OrphanPlan{{
		Topic: "skill/acme-orphan",
		Label: "acme-ghost",
		Candidates: []Candidate{
			{Label: "acme-checkout", Overlap: 0.6},
			{Label: "acme-shipping", Overlap: 0.6},
		},
	}}}

	beforeSync := getRecord(t, store, "skill/acme-orphan").SyncID

	if _, err := ApplyPlanWithOptions(store, plan, clusters, t.TempDir(),
		ApplyOptions{ToCluster: "acme-shipping"}); err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	rec := getRecord(t, store, "skill/acme-orphan")

	// Rebound to the NAMED cluster, not the rival and not left orphaned.
	if got := crystallize.ParseLabel(rec.Content); got != "acme-shipping" {
		t.Errorf("orphan rebound to %q, want the named target %q (not %q)", got, "acme-shipping", "acme-checkout")
	}
	// Source-hash proves it bound to the named cluster's members, not the rival's.
	if got, want := crystallize.ParseSourceHash(rec.Content), crystallize.SourceHash(shipping.Members); got != want {
		t.Errorf("source-hash = %q, want SourceHash(acme-shipping members) = %q", got, want)
	}
	if st := crystallize.RecordStatus(rec, byLabelMap(clusters)); st == crystallize.StatusOrphan {
		t.Errorf("orphan still classifies StatusOrphan after an explicit --to-cluster override")
	}
	// Rebind-only path keeps sync_id stable (no --name).
	if rec.SyncID != beforeSync {
		t.Errorf("sync_id changed on the rebind-only override path: before=%q after=%q", beforeSync, rec.SyncID)
	}
}

// @s3 — a record whose old folder is already gone re-points WITHOUT error. The
// --name rename must tolerate a missing source folder (re-materializing the
// target or binding to an existing one): the apply completes with no error and
// the record is re-pointed to skill/new.
func TestApplyRename_MissingOldFolderNoError(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir() // deliberately empty: no old/ folder on disk

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	if _, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"}); err != nil {
		t.Fatalf("ApplyPlanWithOptions must not fail when the old folder is absent: %v", err)
	}

	// The record was re-pointed despite the missing folder.
	if _, ok := lookupRecord(t, store, "skill/old"); ok {
		t.Errorf("skill/old still present; want it re-pointed to skill/new")
	}
	rec, ok := lookupRecord(t, store, "skill/new")
	if !ok {
		t.Fatalf("record skill/new not found after re-point")
	}
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("re-pointed label = %q, want %q", got, "acme-checkout")
	}
}

// @s4 — marked folders with no live record are removed; hand-written folders are
// not. A crystallize-managed (marked) folder mapping to no live record is deleted;
// a marked folder that DOES map to a live record is kept; and a hand-written
// folder (no marker) is always left byte-for-byte intact.
func TestApplyRename_FolderReconciliation(t *testing.T) {
	store := newStore(t)
	seedSkillRecord(t, store, "acme-keep") // a live record backing the acme-keep folder

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "acme-keep", true)                  // marked + live record  -> kept
	materializeSkill(t, skillsDir, "acme-vanished", true)              // marked, no record     -> removed
	manualPath := materializeSkill(t, skillsDir, "acme-manual", false) // hand-written, no record -> kept
	manualBytes, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatal(err)
	}

	// No orphans to rebind; the folder reconciliation runs regardless.
	if _, err := ApplyPlanWithOptions(store, Plan{}, nil, skillsDir, ApplyOptions{}); err != nil {
		t.Fatalf("ApplyPlanWithOptions: %v", err)
	}

	// The marked orphan folder is removed.
	if isDir(filepath.Join(skillsDir, "acme-vanished")) {
		t.Errorf("marked folder acme-vanished (no live record) was not removed")
	}
	// A marked folder backed by a live record survives.
	if !isDir(filepath.Join(skillsDir, "acme-keep")) {
		t.Errorf("marked folder acme-keep (backed by a live record) was wrongly removed")
	}
	// The hand-written folder is left byte-for-byte intact.
	if !isDir(filepath.Join(skillsDir, "acme-manual")) {
		t.Fatalf("hand-written folder acme-manual was removed; must be preserved")
	}
	got, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(manualBytes) {
		t.Errorf("hand-written SKILL.md was modified:\n before = %q\n after  = %q", manualBytes, got)
	}
}

// @s6 — HARDENING (destination-collision safety). When the --name destination
// folder skillsDir/<Name>/ already exists (a stray/unmanaged directory), the
// apply must validate the destination is FREE *before* any mutation and fail
// atomically: return a non-nil error WITHOUT having retopic'd the record or
// touched either folder. Today the code commits store.Retopic first and only
// then os.Rename the folder, so a pre-existing destination leaves the record
// moved (DB committed) while the folder rename fails — an inconsistent,
// half-applied state. This test pins the SAFE behavior; it is RED against the
// current ordering because skill/old has already been renamed to skill/new.
func TestApplyRename_DestinationFolderCollisionAborts(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "old", true) // crystallize-managed source folder

	// A pre-existing STRAY dir at the destination, with a sentinel file so the
	// collision is unambiguous (a non-empty dir cannot be os.Rename'd onto).
	strayDir := filepath.Join(skillsDir, "new")
	if err := os.MkdirAll(strayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	strayPath := filepath.Join(strayDir, "STRAY.txt")
	strayBytes := []byte("pre-existing unmanaged content; must not be clobbered\n")
	if err := os.WriteFile(strayPath, strayBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	before := getRecord(t, store, "skill/old") // original topic + sync_id

	_, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"})
	if err == nil {
		t.Fatalf("ApplyPlanWithOptions must fail when the destination folder already exists")
	}

	// The record was NOT retopic'd: skill/old is still present at its ORIGINAL
	// sync_id, and skill/new was never minted.
	rec, ok := lookupRecord(t, store, "skill/old")
	if !ok {
		t.Fatalf("skill/old was retopic'd despite the aborted apply; the DB mutated before the collision check")
	}
	if rec.SyncID != before.SyncID {
		t.Errorf("skill/old sync_id changed: before=%q after=%q (record must be left untouched)", before.SyncID, rec.SyncID)
	}
	if _, ok := lookupRecord(t, store, "skill/new"); ok {
		t.Errorf("skill/new was created despite the aborted apply; destination must be validated before mutating")
	}

	// The source folder is untouched (still at old/).
	if !isDir(filepath.Join(skillsDir, "old")) {
		t.Errorf("source folder old/ disappeared after the aborted apply")
	}

	// The stray destination dir is byte-for-byte unchanged.
	if !isDir(strayDir) {
		t.Fatalf("stray destination dir new/ disappeared after the aborted apply")
	}
	got, err := os.ReadFile(strayPath)
	if err != nil {
		t.Fatalf("stray sentinel file missing after aborted apply: %v", err)
	}
	if string(got) != string(strayBytes) {
		t.Errorf("stray destination content was modified:\n before = %q\n after  = %q", strayBytes, got)
	}
}

// @s7 — HARDENING (divergence on the rename path for a hand-written source
// folder). When --name renames the record but the source folder skillsDir/<old>/
// is HAND-WRITTEN (no crystallize marker), the code correctly does NOT rename the
// folder — but today it `continue`s without recording a Divergence, silently
// dropping the fact that a user-owned folder is now orphaned relative to the
// renamed record. The rebind-only path DOES report this; the rename path must
// too. This test pins that: the record still renames to skill/new, the
// hand-written old/ folder is left byte-for-byte intact, AND a Divergence is
// reported pointing at the old hand-written folder. RED because Divergent is
// empty under the current rename path.
func TestApplyRename_HandWrittenSourceFolderReportsDivergence(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	skillsDir := t.TempDir()
	handPath := materializeSkill(t, skillsDir, "old", false) // hand-written: no marker
	handBytes, err := os.ReadFile(handPath)
	if err != nil {
		t.Fatal(err)
	}

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

	// (a) The record still moved: skill/old -> skill/new via Retopic.
	if _, ok := lookupRecord(t, store, "skill/old"); ok {
		t.Errorf("skill/old still present; the record must rename to skill/new even with a hand-written folder")
	}
	if _, ok := lookupRecord(t, store, "skill/new"); !ok {
		t.Fatalf("renamed record skill/new not found in store")
	}

	// (b) The hand-written folder is NOT renamed and is byte-for-byte intact.
	if isDir(filepath.Join(skillsDir, "new")) {
		t.Errorf("hand-written folder was relocated to new/; a user-owned folder must never be moved")
	}
	if !isDir(filepath.Join(skillsDir, "old")) {
		t.Fatalf("hand-written folder old/ disappeared; it must be preserved")
	}
	got, err := os.ReadFile(handPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(handBytes) {
		t.Errorf("hand-written SKILL.md was modified:\n before = %q\n after  = %q", handBytes, got)
	}

	// (c) The now-orphaned hand-written folder is reported as a Divergence
	// pointing at the OLD folder's SKILL.md, keyed by the renamed record.
	var found *Divergence
	for i := range res.Divergent {
		if res.Divergent[i].Path == handPath {
			found = &res.Divergent[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no Divergence reported for the hand-written source folder %q; Divergent = %+v", handPath, res.Divergent)
	}
	if found.Topic != "skill/new" {
		t.Errorf("Divergence.Topic = %q, want the renamed record %q", found.Topic, "skill/new")
	}
}

// @s8 — HARDENING (destination RECORD collision, atomicity). The folder-collision
// pre-check only guards skillsDir/<Name>/; it does NOT guard a pre-existing
// destination RECORD at topic skill/<Name>. When such a record already exists but
// no managed folder sits at skillsDir/<Name>/, today's flow is: folder check
// passes -> the provenance Upsert on skill/<old> COMMITS (rewriting the orphan's
// content + bumping its revision) -> store.Retopic then fails on its own in-tx
// collision guard because skill/<Name> is taken -> a non-nil error is returned,
// BUT the premature Upsert has already committed. Net: skill/<old>'s provenance
// was rewritten (content/revision/updated_at changed) even though the whole op
// errored — not atomic. This test pins the SAFE behavior: on error, skill/<old>
// must be byte-for-byte and revision-for-revision UNCHANGED (the Upsert must not
// have fired), and the pre-existing skill/<Name> must be untouched too. RED
// against the current ordering because skill/<old>'s content/revision have
// already changed by the time the collision aborts.
func TestApplyRename_DestinationRecordCollisionAbortsAtomically(t *testing.T) {
	store := newStore(t)
	seedOrphan(t, store, "old", "acme-ghost")

	// A SECOND, pre-existing NON-DELETED record already occupies the rename
	// destination topic skill/new. There is deliberately NO folder at
	// skillsDir/new/, so the folder-collision pre-check cannot catch this.
	seedSkillRecord(t, store, "new")

	skillsDir := t.TempDir()
	materializeSkill(t, skillsDir, "old", true) // crystallize-managed source folder (fine)

	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}

	beforeOld := getRecord(t, store, "skill/old") // content + provenance + sync_id + revision
	beforeNew := getRecord(t, store, "skill/new") // the pre-existing destination record

	// (a) The op must fail: skill/new is already taken, so the rename cannot land.
	_, err := ApplyPlanWithOptions(store, plan, clusters, skillsDir,
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"})
	if err == nil {
		t.Fatalf("ApplyPlanWithOptions must fail when a record already exists at the rename destination skill/new")
	}

	// (b) skill/old is UNCHANGED — the premature provenance Upsert did NOT happen.
	afterOld, ok := lookupRecord(t, store, "skill/old")
	if !ok {
		t.Fatalf("skill/old is gone after the aborted apply; it must be left untouched at its original topic")
	}
	if afterOld.Content != beforeOld.Content {
		t.Errorf("skill/old content was rewritten despite the aborted apply:\n before = %q\n after  = %q", beforeOld.Content, afterOld.Content)
	}
	if got, want := crystallize.ParseLabel(afterOld.Content), crystallize.ParseLabel(beforeOld.Content); got != want {
		t.Errorf("skill/old provenance label changed: before=%q after=%q (record must be untouched)", want, got)
	}
	if afterOld.SyncID != beforeOld.SyncID {
		t.Errorf("skill/old sync_id changed: before=%q after=%q", beforeOld.SyncID, afterOld.SyncID)
	}
	if afterOld.Revision != beforeOld.Revision {
		t.Errorf("skill/old revision bumped despite the aborted apply: before=%d after=%d (the Upsert must not have committed)", beforeOld.Revision, afterOld.Revision)
	}

	// (c) The pre-existing skill/new record is unchanged too.
	afterNew, ok := lookupRecord(t, store, "skill/new")
	if !ok {
		t.Fatalf("pre-existing skill/new disappeared after the aborted apply")
	}
	if afterNew.Content != beforeNew.Content {
		t.Errorf("pre-existing skill/new content was modified:\n before = %q\n after  = %q", beforeNew.Content, afterNew.Content)
	}
	if afterNew.SyncID != beforeNew.SyncID || afterNew.Revision != beforeNew.Revision {
		t.Errorf("pre-existing skill/new identity changed: sync before=%q after=%q, rev before=%d after=%d",
			beforeNew.SyncID, afterNew.SyncID, beforeNew.Revision, afterNew.Revision)
	}
}

// @s5 — rename team-sync semantics are pinned (decision D7). A teammate's fresh
// store that already imported the PRE-rename record then imports the POST-rename
// export: the renamed record is reproduced under the NEW sync_id, and the OLD
// sync_id row is NOT auto-deleted by import (the documented lingering behavior).
func TestApplyRename_TeamSyncLingersOldSyncID(t *testing.T) {
	source := newStore(t)
	seedOrphan(t, source, "old", "acme-ghost")

	// A teammate imports the pre-rename record (skill/old @ old sync_id).
	pre, err := source.ExportRecords("alice")
	if err != nil {
		t.Fatalf("ExportRecords (pre): %v", err)
	}
	mate := newStore(t)
	if _, err := mate.ImportRecords(pre); err != nil {
		t.Fatalf("ImportRecords (pre): %v", err)
	}
	oldSync := syncIDFor(t, "skill/old")
	if r, ok := lookupRecord(t, mate, "skill/old"); !ok || r.SyncID != oldSync {
		t.Fatalf("teammate did not receive skill/old @ old sync_id: ok=%v rec=%+v", ok, r)
	}

	// The source renames the record.
	target := clus("acme-checkout",
		"testing/checkout-flow", "gotcha/checkout-null", "decision/checkout-tax")
	clusters := []crystallize.Cluster{target}
	plan := Plan{Orphans: []OrphanPlan{{
		Topic:      "skill/old",
		Label:      "acme-ghost",
		Candidates: []Candidate{{Label: "acme-checkout", Overlap: 0.6}},
	}}}
	if _, err := ApplyPlanWithOptions(source, plan, clusters, t.TempDir(),
		ApplyOptions{Name: "new", ToCluster: "acme-checkout"}); err != nil {
		t.Fatalf("ApplyPlanWithOptions (rename): %v", err)
	}

	// The teammate imports the post-rename export.
	post, err := source.ExportRecords("alice")
	if err != nil {
		t.Fatalf("ExportRecords (post): %v", err)
	}
	if _, err := mate.ImportRecords(post); err != nil {
		t.Fatalf("ImportRecords (post): %v", err)
	}

	// The renamed record is reproduced under the NEW sync_id.
	newSync := syncIDFor(t, "skill/new")
	rec, ok := lookupRecord(t, mate, "skill/new")
	if !ok {
		t.Fatalf("renamed record skill/new not reproduced on the teammate store")
	}
	if rec.SyncID != newSync {
		t.Errorf("reproduced sync_id = %q, want computeSyncID(project, skill/new) = %q", rec.SyncID, newSync)
	}
	if got := crystallize.ParseLabel(rec.Content); got != "acme-checkout" {
		t.Errorf("reproduced label = %q, want %q", got, "acme-checkout")
	}

	// The OLD sync_id row is NOT auto-deleted by import (D7 lingering behavior).
	stale, ok := lookupRecord(t, mate, "skill/old")
	if !ok {
		t.Errorf("old-sync_id row skill/old was auto-deleted by import; D7 says it must linger")
	} else if stale.SyncID != oldSync {
		t.Errorf("lingering row sync_id = %q, want the original %q", stale.SyncID, oldSync)
	}
}
