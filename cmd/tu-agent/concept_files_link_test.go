package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// P2 — the learn pipeline must wire the member files it already computes
// (codegen.Concept.Files) through to the graph store. Without this the store
// has concepts but no idea which files belong to them, and `status` cannot
// hash a concept to detect staleness.

// @s3 — a codegen.Concept whose Files field lists a source file, written
// through the concept-persistence path learn uses (persistConceptCards), reads
// back from the graph store with that file as a member.
func TestPersistConceptCards_WiresConceptFilesIntoStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(dir)

	cards := []codegen.ConceptCard{{
		Concept: codegen.Concept{
			Name:    "billing",
			Package: "internal/billing",
			Files:   []string{"internal/billing/invoice.go"},
		},
		Definition: "invoicing and tax",
	}}
	if err := persistConceptCards(cards); err != nil {
		t.Fatalf("persistConceptCards: %v", err)
	}

	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("openGraphStore: %v", err)
	}
	defer st.Close()

	row, ok, err := st.GetConcept("billing")
	if err != nil || !ok {
		t.Fatalf("GetConcept(billing): ok=%v err=%v", ok, err)
	}
	if len(row.Files) != 1 || row.Files[0] != "internal/billing/invoice.go" {
		t.Errorf("stored concept Files = %v, want [internal/billing/invoice.go]", row.Files)
	}
}

// @s3 — the wiring must carry each card's own files, not a flattened or shared
// list: two concepts persisted in one call keep disjoint member sets.
func TestPersistConceptCardsTo_KeepsPerConceptFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, ".tu-agent", "graph.db"), "test-ext")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cards := []codegen.ConceptCard{
		{Concept: codegen.Concept{Name: "billing", Package: "internal/billing",
			Files: []string{"internal/billing/invoice.go", "internal/billing/tax.go"}},
			Definition: "invoicing and tax"},
		{Concept: codegen.Concept{Name: "shipping", Package: "internal/shipping",
			Files: []string{"internal/shipping/rate.go"}},
			Definition: "rate quoting"},
	}
	if err := persistConceptCardsTo(st, cards); err != nil {
		t.Fatalf("persistConceptCardsTo: %v", err)
	}

	tests := []struct {
		concept string
		want    []string
	}{
		{"billing", []string{"internal/billing/invoice.go", "internal/billing/tax.go"}},
		{"shipping", []string{"internal/shipping/rate.go"}},
	}
	for _, tc := range tests {
		t.Run(tc.concept, func(t *testing.T) {
			row, ok, err := st.GetConcept(tc.concept)
			if err != nil || !ok {
				t.Fatalf("GetConcept(%q): ok=%v err=%v", tc.concept, ok, err)
			}
			if len(row.Files) != len(tc.want) {
				t.Fatalf("GetConcept(%q).Files = %v, want %v", tc.concept, row.Files, tc.want)
			}
			for i := range tc.want {
				if row.Files[i] != tc.want[i] {
					t.Errorf("GetConcept(%q).Files = %v, want %v", tc.concept, row.Files, tc.want)
					break
				}
			}
		})
	}
}
