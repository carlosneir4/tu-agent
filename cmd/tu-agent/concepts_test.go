package main

import (
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
	cards, err := buildConceptCardsFromUnits(units, nil, nil, []string{"com.acme.shop"}, codegen.DomainMapOptions{Depth: 1, MinFiles: 1}, "leiden")
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
	cards, err := buildConceptCardsFromUnits(units, nil, nil, nil, codegen.DomainMapOptions{Depth: 1, MinFiles: 1}, "heuristic")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) == 0 {
		t.Fatal("fallback produced no cards")
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
