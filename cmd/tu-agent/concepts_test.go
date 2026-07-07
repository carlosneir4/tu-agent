package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/graph/store"
)

// persistConceptCardsTo must not clobber a previously generated (LLM) description
// when re-run without a model: a card with Definition == "" should preserve the
// existing non-fallback description already in the store. Regression test for the
// learn --skip-llm description-degradation bug.
func TestPersistConceptCardsTo_PreservesExistingLLMDescription(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, ".tu-agent"), 0o755)
	st, err := store.Open(filepath.Join(tmpDir, ".tu-agent", "graph.db"), "test-ext")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Seed the store with a good, model-authored description.
	seed := []store.ConceptRow{{
		Name:        "memory",
		Description: "The persistent memory store — SQLite-backed durable notes.",
		Content: "---\nname: memory\n" +
			"description: The persistent memory store — SQLite-backed durable notes.\n" +
			"---\n\n# memory (internal/memory)\n",
	}}
	if err := st.ReplaceConcepts(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A model-free re-run produces a card with no Definition (deterministic).
	card := codegen.ConceptCard{
		Concept:    codegen.Concept{Name: "memory", Package: "internal/memory", Files: []string{"internal/memory/store.go"}},
		Definition: "",
		Landmarks:  []codegen.Landmark{{Name: "store.go", Path: "internal/memory/store.go"}},
	}
	if err := persistConceptCardsTo(st, []codegen.ConceptCard{card}); err != nil {
		t.Fatalf("persistConceptCardsTo: %v", err)
	}

	row, ok, err := st.GetConcept("memory")
	if err != nil || !ok {
		t.Fatalf("GetConcept: ok=%v err=%v", ok, err)
	}
	if got := row.Description; got != "The persistent memory store — SQLite-backed durable notes." {
		t.Errorf("description was clobbered; got %q", got)
	}
}

// When the stored description is itself a deterministic fallback, a model-free
// re-run is free to refresh it (no information loss).
func TestPersistConceptCardsTo_RefreshesExistingFallback(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, ".tu-agent"), 0o755)
	st, err := store.Open(filepath.Join(tmpDir, ".tu-agent", "graph.db"), "test-ext")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Seed with a stale deterministic description.
	seed := []store.ConceptRow{{
		Name:        "memory",
		Description: "internal/memory — 2 files; landmarks: old.go",
		Content:     "---\nname: memory\ndescription: \"internal/memory — 2 files; landmarks: old.go\"\n---\n\n# memory\n",
	}}
	if err := st.ReplaceConcepts(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	card := codegen.ConceptCard{
		Concept:   codegen.Concept{Name: "memory", Package: "internal/memory", Files: []string{"a.go", "b.go", "c.go"}},
		Landmarks: []codegen.Landmark{{Name: "store.go", Path: "internal/memory/store.go"}},
	}
	if err := persistConceptCardsTo(st, []codegen.ConceptCard{card}); err != nil {
		t.Fatalf("persistConceptCardsTo: %v", err)
	}

	row, ok, err := st.GetConcept("memory")
	if err != nil || !ok {
		t.Fatalf("GetConcept: ok=%v err=%v", ok, err)
	}
	if got := row.Description; got == "internal/memory — 2 files; landmarks: old.go" {
		t.Errorf("stale fallback was not refreshed; got %q", got)
	}
	if !codegen.IsDeterministicDescription(row.Description) {
		t.Errorf("expected a refreshed deterministic description, got %q", row.Description)
	}
}

func TestBuildConceptCardsFromUnits(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "src/c/P.java", Package: "com.acme.shop.catalog"},
		{Path: "src/o/O.java", Package: "com.acme.shop.orders"},
	}
	cards, err := buildConceptCardsFromUnits(units, nil, nil, nil, nil, []string{"com.acme.shop"}, codegen.DomainMapOptions{Depth: 1, MinFiles: 1}, "leiden")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Name != "catalog" || cards[1].Name != "orders" {
		t.Fatalf("cards = %+v", cards)
	}
}

func TestBuildConceptCardsFallsBackToDomains(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "a1", Package: "com.acme.x"}, {Path: "a2", Package: "com.acme.x"},
		{Path: "a3", Package: "com.acme.x"}, {Path: "a4", Package: "com.acme.x"},
		{Path: "a5", Package: "com.acme.y"},
	}
	cards, err := buildConceptCardsFromUnits(units, nil, nil, nil, nil, nil, codegen.DomainMapOptions{Depth: 1, MinFiles: 1}, "heuristic")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) == 0 {
		t.Fatal("fallback produced no cards")
	}
}

// TestBuildConceptCardsThreadsWeighted is the regression test for the two-layer
// drop that made `learn --cluster leiden` silently never run Leiden: learn.go
// discarded edges/weighted from loadSourceUnits, and buildConceptCardsFromUnits
// hardcoded nil/nil into buildDomains, so BuildDomainMapClustered always took
// its len(weighted)==0 fallback regardless of the flag. All 20 units share one
// package (so the package-path heuristic can only ever produce a single
// domain), but the weighted edges form two internally-dense, mutually
// disconnected communities — so if edges/weighted actually reach
// BuildDomainMapClustered, its topology split (2 domains) must differ from the
// heuristic's package split (1 domain), and must match calling
// BuildDomainMapClustered directly.
func TestBuildConceptCardsThreadsWeighted(t *testing.T) {
	const groupSize = 10 // 2 groups * 10 = 20 >= clusterFallbackMinFiles
	var units []codegen.SourceUnit
	var weighted []codegen.WeightedEdge
	for _, group := range []string{"a", "b"} {
		for i := 1; i <= groupSize; i++ {
			units = append(units, codegen.SourceUnit{
				Path:    fmt.Sprintf("src/%s%d.go", group, i),
				Package: "pkg.shared",
			})
			if i > 1 {
				weighted = append(weighted, codegen.WeightedEdge{
					From:   fmt.Sprintf("src/%s%d.go", group, i-1),
					To:     fmt.Sprintf("src/%s%d.go", group, i),
					Weight: 5,
				})
			}
		}
	}
	opts := codegen.DomainMapOptions{Depth: 1, MinFiles: 1}

	cards, err := buildConceptCardsFromUnits(units, nil, weighted, nil, nil, nil, opts, "leiden")
	if err != nil {
		t.Fatal(err)
	}
	gotNames := map[string]bool{}
	for _, c := range cards {
		gotNames[c.Name] = true
	}

	wantDomains := codegen.BuildDomainMapClustered(units, nil, weighted, opts)
	wantNames := map[string]bool{}
	for _, d := range wantDomains {
		if d.Files == nil {
			continue // parent marker, not a leaf concept
		}
		wantNames[d.Name] = true
	}
	if len(gotNames) != len(wantNames) {
		t.Fatalf("cards = %v, want name set %v (from BuildDomainMapClustered directly)", gotNames, wantNames)
	}
	for name := range wantNames {
		if !gotNames[name] {
			t.Errorf("card set %v missing clustered domain %q", gotNames, name)
		}
	}

	// The heuristic groups every file under one shared package into a single
	// domain; if the community split actually reached the pipeline, it must
	// diverge from that (two domains vs one).
	heuristicDomains := codegen.BuildDomainMap(units, nil, opts)
	heurLeaves := 0
	for _, d := range heuristicDomains {
		if d.Files != nil {
			heurLeaves++
		}
	}
	if heurLeaves == len(wantNames) {
		t.Fatalf("fixture did not diverge: heuristic leaves=%d, clustered leaves=%d", heurLeaves, len(wantNames))
	}
}

func TestPersistConceptCards(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755)
	st, err := store.Open(filepath.Join(dir, ".tu-agent", "graph.db"), "test-ext")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cards := []codegen.ConceptCard{
		{Concept: codegen.Concept{Name: "widgets"}, Definition: "widget rendering"},
	}
	if err := persistConceptCardsTo(st, cards); err != nil {
		t.Fatalf("persistConceptCardsTo: %v", err)
	}

	row, ok, err := st.GetConcept("widgets")
	if err != nil || !ok {
		t.Fatalf("GetConcept: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(row.Content, "widgets") {
		t.Errorf("stored content missing card text: %q", row.Content)
	}
	if row.Description == "" {
		t.Errorf("Description column empty; want the frontmatter description for a card with Definition set")
	}
	// No concept skill dir was written to disk.
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "skills", "widgets")); !os.IsNotExist(statErr) {
		t.Errorf(".claude/skills/widgets should not exist; statErr=%v", statErr)
	}
}

func TestSetConceptDefinition(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755)
	t.Chdir(dir)
	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "acme.widgets — 12 files; landmarks: Widget",
			Content: "---\nname: widgets\ndescription: acme.widgets — 12 files; landmarks: Widget\n---\n\n# widgets\n\nLandmarks:\n- Widget\n"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	st.Close()

	if err := setConceptDefinition("widgets", "Reusable UI blocks composed into pages."); err != nil {
		t.Fatalf("setConceptDefinition: %v", err)
	}

	st2, _ := openGraphStore()
	defer st2.Close()
	row, ok, _ := st2.GetConcept("widgets")
	if !ok {
		t.Fatal("concept missing after set")
	}
	if row.Description != "Reusable UI blocks composed into pages." {
		t.Errorf("description column not updated: %q", row.Description)
	}
	if !strings.Contains(row.Content, "description: Reusable UI blocks composed into pages.") {
		t.Errorf("content frontmatter not updated: %q", row.Content)
	}
	if !strings.Contains(row.Content, "- Widget") {
		t.Errorf("content body not preserved: %q", row.Content)
	}

	// Unknown concept errors.
	if err := setConceptDefinition("nope", "x"); err == nil {
		t.Errorf("expected error for unknown concept")
	}
}
