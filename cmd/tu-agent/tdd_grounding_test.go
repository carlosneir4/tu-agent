package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// groundingRepoDirName is the temp repo's directory name. It is deliberately
// generic and feature-free — the spec mandates that grounding keywords derive
// from the per-feature BASE dir (relBase, the --base flag), never from
// repoRoot(). A repo dir that happened to carry the feature's words would let
// an implementation that (wrongly) keys off repoRoot()'s basename pass this
// test for the wrong reason.
const groundingRepoDirName = "acme-repo"

// groundingFeatureSlug is the per-feature base-dir name whose hyphen-split
// words ("widget", "checkout", "retry") must match the seeded decision note
// so the slug->keywords derivation described in the design (D3) finds it via
// memory.Store.Search. It is used as the leaf of groundingFeatureBase, the
// --base value passed to `tdd prompt`.
const groundingFeatureSlug = "widget-checkout-retry"

// groundingFeatureBase is the --base value every grounded-fixture test passes
// to tdd prompt. filepath.Base(groundingFeatureBase) == groundingFeatureSlug
// is what the keyword derivation must key off.
const groundingFeatureBase = ".tu-agent/tdd/" + groundingFeatureSlug

// groundingDecisionTitle is the seeded decision note's title/topic — the exact
// text @s1 asserts appears in the composed "architect" prompt once grounding
// is spliced in.
const groundingDecisionTitle = "Use exponential backoff for widget checkout retry failures"

// groundingGapSymbol is the name of the single untested exported function
// seeded into the graph store, standing in for the "blast radius & test gaps"
// grounding source.
const groundingGapSymbol = "GapFunc"

// groundingBeyondCapMarker is placed only past the 2048-byte cap in the
// oversized architecture overview seeded for @s4, so its absence from the
// composed output proves the source was actually truncated, not merely
// labeled as such.
const groundingBeyondCapMarker = "ZZZ_BEYOND_THE_2KB_CAP_ZZZ"

// chdirTemp changes the working directory to dir for the duration of the test
// and restores the original working directory on cleanup. repoRoot() (and the
// store helpers that call it internally, e.g. openGraphStore/loadArchitecture)
// resolve relative to os.Getwd(), so the fixture repo must be the cwd while
// tddPromptCmd.RunE runs — mirrors the pattern in TestRunGraphBuildFromSubdir.
func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// seedGroundingGraph opens (creating if absent) the graph store under root and
// writes: the architecture-overview narrative under the same metadata key
// loadArchitecture reads, and one untested exported function (span 10, no
// test/coverage edges) so query.Graph.UntestedGaps reports it as a gap — the
// minimal fixture for the "blast radius & test gaps" grounding source (see
// gapFixture in internal/graph/query/untested_test.go for the shape this
// mirrors).
func seedGroundingGraph(t *testing.T, root, archOverview string) {
	t.Helper()
	dbPath := filepath.Join(root, ".tu-agent", "graph", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// extract.ExtractorVersion must match what openGraphStore uses internally
	// (cmd/tu-agent/graph.go), or the version-mismatch path in store.Open wipes
	// this seed data on the next open from within RunE.
	st, err := store.Open(dbPath, extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("seedGroundingGraph: open: %v", err)
	}
	defer st.Close()

	if archOverview != "" {
		if err := st.SetMeta(architectureMetaKey, archOverview); err != nil {
			t.Fatalf("seedGroundingGraph: SetMeta: %v", err)
		}
	}

	rec := store.FileRecord{Path: "svc.go", SHA256: "seed", Language: "go", Status: "ok"}
	nodes := []graph.Node{
		{ID: "svc.go::" + groundingGapSymbol, Kind: graph.KindFunction, Name: groundingGapSymbol,
			Path: "svc.go", Line: 1, EndLine: 10, Language: "go", Exported: true},
	}
	if err := st.ReplaceFileAndNodes(rec, nodes, nil, nil); err != nil {
		t.Fatalf("seedGroundingGraph: ReplaceFileAndNodes: %v", err)
	}
}

// seedGroundingMemory opens (creating if absent) the memory store under root
// and upserts one decision note whose title/content match the
// groundingFeatureSlug keywords — the fixture for the "relevant decisions &
// gotchas" grounding source.
func seedGroundingMemory(t *testing.T, root string) {
	t.Helper()
	dbPath := filepath.Join(root, ".tu-agent", "memory", "memory.db")
	st, err := memory.Open(dbPath)
	if err != nil {
		t.Fatalf("seedGroundingMemory: open: %v", err)
	}
	defer st.Close()

	content := "Decision: for widget checkout retry failures, use exponential " +
		"backoff instead of a fixed delay so a transient outage does not " +
		"stampede the payment gateway. Alternatives rejected: fixed delay " +
		"(thundering herd), unlimited retries (masks real outages)."
	_, err = st.Upsert("decision/widget-checkout-retry", content, memory.UpsertOpts{
		Type:  "decision",
		Title: groundingDecisionTitle,
	})
	if err != nil {
		t.Fatalf("seedGroundingMemory: Upsert: %v", err)
	}
}

// seedGroundingRules writes a minimal repo-wide project rules file, so the
// "## Project rules" header actually appears in the composed prompt — without
// it, loadProjectRules returns "" and @s5's ordering assertion (grounding
// after the project-rules header) would be vacuous.
func seedGroundingRules(t *testing.T, root string) {
	t.Helper()
	rulesDir := filepath.Join(root, ".tu-agent", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "all.md"), []byte("Follow generic project conventions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newGroundedFixture builds a temp repo — basename groundingRepoDirName,
// deliberately generic (see its doc comment) — with seeded graph and memory
// stores, a project rules file, and the per-feature base dir
// (groundingFeatureBase) that carries the keyword-bearing slug. It chdirs
// into the repo root for the duration of the test and returns it.
func newGroundedFixture(t *testing.T, archOverview string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), groundingRepoDirName)
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedGroundingGraph(t, root, archOverview)
	seedGroundingMemory(t, root)
	seedGroundingRules(t, root)
	// The per-feature base dir itself: real per-feature dirs hold spec.md /
	// plan.md / features/, so it must exist on disk, not just as a string.
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(groundingFeatureBase)), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTemp(t, root)
	return root
}

// runTddPrompt invokes tddPromptCmd.RunE for stage against whatever repo is
// currently the working directory (repoRoot() resolves from os.Getwd()),
// pinning --base to the given explicit value so PromptRelBase never falls
// through to ticket/description slugification. Grounded-fixture tests pass
// groundingFeatureBase so filepath.Base(base) matches the seeded decision
// note's keywords; @s3 (no stores) passes it too for consistency, though no
// notes exist there to match.
func runTddPrompt(t *testing.T, base, stage string) (string, error) {
	t.Helper()
	origBase := tddPromptBase
	t.Cleanup(func() { tddPromptBase = origBase })
	tddPromptBase = base

	var buf bytes.Buffer
	tddPromptCmd.SetOut(&buf)
	err := tddPromptCmd.RunE(tddPromptCmd, []string{stage})
	return buf.String(), err
}

// @s1 — architect prompt carries the full grounding block: header, an
// architecture-overview excerpt, the seeded decision note title, and a
// blast-radius/test-gap line.
//
// RED today: ComposeStagePrompt (the function tddPromptCmd.RunE currently
// calls) never splices any grounding text, so none of these substrings are
// present. This test compiles against current code and fails at runtime —
// honest red per the D3 acceptance #1/#4.
func TestGroundingInjection_S1_ArchitectCarriesFullBlock(t *testing.T) {
	overview := "The payments service handles widget checkout retry orchestration."
	newGroundedFixture(t, overview)

	out, err := runTddPrompt(t, groundingFeatureBase, "architect")
	if err != nil {
		t.Fatalf("tdd prompt architect: %v", err)
	}
	if !strings.Contains(out, "## Project grounding") {
		t.Errorf("architect prompt missing grounding header; got:\n%s", out)
	}
	if !strings.Contains(out, "widget checkout retry orchestration") {
		t.Errorf("architect prompt missing architecture-overview excerpt; got:\n%s", out)
	}
	if !strings.Contains(out, groundingDecisionTitle) {
		t.Errorf("architect prompt missing seeded decision note title %q; got:\n%s", groundingDecisionTitle, out)
	}
	if !strings.Contains(out, groundingGapSymbol) {
		t.Errorf("architect prompt missing blast-radius/test-gap line for %q; got:\n%s", groundingGapSymbol, out)
	}
}

// @s2 — grounding is scoped to planning stages only: analyst gets it,
// implementer does not.
//
// RED today for the analyst half (no grounding is ever spliced, for any
// stage) — an honest compile-safe red. The implementer half is a legitimate
// green-today pin (the header has never existed anywhere), kept in the same
// scenario because the feature file pairs them as one behavior.
func TestGroundingInjection_S2_ScopedToPlanningStagesOnly(t *testing.T) {
	newGroundedFixture(t, "Architecture excerpt for scoping test.")

	analystOut, err := runTddPrompt(t, groundingFeatureBase, "analyst")
	if err != nil {
		t.Fatalf("tdd prompt analyst: %v", err)
	}
	if !strings.Contains(analystOut, "## Project grounding") {
		t.Errorf("analyst prompt missing grounding header; got:\n%s", analystOut)
	}

	implementerOut, err := runTddPrompt(t, groundingFeatureBase, "implementer")
	if err != nil {
		t.Fatalf("tdd prompt implementer: %v", err)
	}
	if strings.Contains(implementerOut, "## Project grounding") {
		t.Errorf("implementer prompt must NOT contain the grounding header; got:\n%s", implementerOut)
	}
}

// @s3 — fail-soft when no graph.db and no memory.db exist: the command still
// succeeds and simply omits the grounding section.
//
// Green today: with no grounding wired in at all, the command already
// succeeds and already lacks the header — a legitimate pin for the
// fail-soft contract the GREEN change must preserve.
func TestGroundingInjection_S3_FailSoftWithNoStores(t *testing.T) {
	root := filepath.Join(t.TempDir(), "no-stores-repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTemp(t, root)

	out, err := runTddPrompt(t, groundingFeatureBase, "architect")
	if err != nil {
		t.Fatalf("tdd prompt architect with no stores should succeed, got err: %v", err)
	}
	if strings.Contains(out, "## Project grounding") {
		t.Errorf("prompt should not contain a grounding header with no stores; got:\n%s", out)
	}
}

// @s4 — an oversized architecture-overview source is cut at the 2048-byte cap
// and carries "(truncated)"; bytes beyond the cap never reach the output.
//
// RED today: no grounding (and so no truncation marker, and no cap) exists at
// all — compiles, fails at runtime.
func TestGroundingInjection_S4_OversizedSourceTruncatedAtCap(t *testing.T) {
	// Build an overview whose first 2048 bytes are innocuous filler and whose
	// tail (well past the cap) carries a unique marker, so "marker absent"
	// proves truncation rather than coincidence.
	filler := strings.Repeat("A", 2200)
	oversized := filler + " " + groundingBeyondCapMarker
	if len(oversized) <= 2048 {
		t.Fatalf("fixture overview too short to exercise the 2KB cap: %d bytes", len(oversized))
	}
	newGroundedFixture(t, oversized)

	out, err := runTddPrompt(t, groundingFeatureBase, "architect")
	if err != nil {
		t.Fatalf("tdd prompt architect: %v", err)
	}
	if !strings.Contains(out, "(truncated)") {
		t.Errorf("architect prompt missing the truncation marker for an oversized source; got:\n%s", out)
	}
	if strings.Contains(out, groundingBeyondCapMarker) {
		t.Errorf("architect prompt contains bytes beyond the 2048-byte cap (marker %q found); got:\n%s", groundingBeyondCapMarker, out)
	}
}

// @s5 — composition order is preserved: the grounding header sits after the
// project-rules header and before the architect stage-overlay contract, and
// the pre-existing "MUST consult the graph" instruction text survives.
//
// RED today: the grounding header never appears at all, so the ordering
// assertion (which requires finding it) fails — compiles, fails at runtime.
func TestGroundingInjection_S5_OrderPreservedAndMustConsultTextIntact(t *testing.T) {
	newGroundedFixture(t, "Architecture excerpt for ordering test.")

	out, err := runTddPrompt(t, groundingFeatureBase, "architect")
	if err != nil {
		t.Fatalf("tdd prompt architect: %v", err)
	}

	idxRules := strings.Index(out, "## Project rules")
	idxGrounding := strings.Index(out, "## Project grounding")
	// "tu-agent TDD task — architect stage" is the literal opening line of
	// ArchitectPrompt (internal/tdd/stage.go) — the stage overlay — and
	// appears nowhere else in the composed prompt, so its index marks where
	// the overlay begins.
	idxOverlay := strings.Index(out, "tu-agent TDD task — architect stage")

	if idxRules < 0 {
		t.Fatalf("expected project-rules header in output; got:\n%s", out)
	}
	if idxGrounding < 0 {
		t.Fatalf("expected grounding header in output; got:\n%s", out)
	}
	if idxOverlay < 0 {
		t.Fatalf("expected architect stage-overlay contract in output; got:\n%s", out)
	}
	if !(idxRules < idxGrounding && idxGrounding < idxOverlay) {
		t.Errorf("expected order project-rules(%d) < grounding(%d) < overlay(%d); got:\n%s",
			idxRules, idxGrounding, idxOverlay, out)
	}

	if !strings.Contains(out, "MUST consult the graph for blast-radius") {
		t.Errorf("existing MUST-consult-the-graph text must remain in the architect prompt; got:\n%s", out)
	}
}
