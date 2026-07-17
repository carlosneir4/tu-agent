package store

import (
	"path/filepath"
	"testing"
)

// P2 — the concept -> member-files link.
//
// ReplaceConcepts today inserts only name/description/content and discards the
// member-file list that the clustering already computes. These tests pin the
// link: a concept's member files survive a round-trip, and the wholesale
// replace drops the links of concepts that are gone.
//
// They are deliberately schema-agnostic: they only use the exported store API
// (ConceptRow.Files + ReplaceConcepts/GetConcept/ListConcepts), so the
// implementation is free to choose a side table or a column.

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "g.db"), "test-ext")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// equalStrings reports whether got and want hold the same paths in the same
// order. Files are stored sorted by path, so order is part of the contract.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// @s1 — a concept stored with member files reads back listing exactly those
// files, through both read paths (GetConcept and ListConcepts).
func TestReplaceConcepts_MemberFilesRoundTrip(t *testing.T) {
	billingFiles := []string{"internal/billing/invoice.go", "internal/billing/tax.go"}

	tests := []struct {
		name      string
		concept   string
		wantFiles []string
	}{
		{
			name:      "concept with two member files",
			concept:   "billing",
			wantFiles: billingFiles,
		},
		{
			name:      "concept with one member file",
			concept:   "shipping",
			wantFiles: []string{"internal/shipping/rate.go"},
		},
		{
			name:      "concept with no member files",
			concept:   "empty",
			wantFiles: nil,
		},
	}

	s := openTestStore(t)
	rows := []ConceptRow{
		{Name: "billing", Description: "invoicing and tax", Content: "---\nname: billing\n---\nbody A", Files: billingFiles},
		{Name: "shipping", Description: "rate quoting", Content: "---\nname: shipping\n---\nbody B", Files: []string{"internal/shipping/rate.go"}},
		{Name: "empty", Description: "no files yet", Content: "---\nname: empty\n---\nbody C"},
	}
	if err := s.ReplaceConcepts(rows); err != nil {
		t.Fatalf("ReplaceConcepts: %v", err)
	}

	byName := map[string]ConceptRow{}
	list, err := s.ListConcepts()
	if err != nil {
		t.Fatalf("ListConcepts: %v", err)
	}
	for _, r := range list {
		byName[r.Name] = r
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := s.GetConcept(tc.concept)
			if err != nil || !ok {
				t.Fatalf("GetConcept(%q): ok=%v err=%v", tc.concept, ok, err)
			}
			if !equalStrings(got.Files, tc.wantFiles) {
				t.Errorf("GetConcept(%q).Files = %v, want %v", tc.concept, got.Files, tc.wantFiles)
			}
			listed, ok := byName[tc.concept]
			if !ok {
				t.Fatalf("ListConcepts missing %q", tc.concept)
			}
			if !equalStrings(listed.Files, tc.wantFiles) {
				t.Errorf("ListConcepts()[%q].Files = %v, want %v", tc.concept, listed.Files, tc.wantFiles)
			}
		})
	}
}

// @s1 — the round-trip must not disturb the columns ReplaceConcepts already
// persists.
func TestReplaceConcepts_MemberFilesDoNotDisturbOtherColumns(t *testing.T) {
	s := openTestStore(t)
	if err := s.ReplaceConcepts([]ConceptRow{{
		Name:        "billing",
		Description: "invoicing and tax",
		Content:     "---\nname: billing\n---\nbody A",
		Files:       []string{"internal/billing/invoice.go", "internal/billing/tax.go"},
	}}); err != nil {
		t.Fatalf("ReplaceConcepts: %v", err)
	}
	got, ok, err := s.GetConcept("billing")
	if err != nil || !ok {
		t.Fatalf("GetConcept: ok=%v err=%v", ok, err)
	}
	if got.Description != "invoicing and tax" {
		t.Errorf("Description = %q, want %q", got.Description, "invoicing and tax")
	}
	if got.Content != "---\nname: billing\n---\nbody A" {
		t.Errorf("Content = %q", got.Content)
	}
}

// @s2 — the wholesale replace drops the file links of concepts that are no
// longer in the set. A stale link is observable when the name comes back: a
// later replace that re-adds "billing" with a different single file must list
// exactly that file, never the files of the dropped generation.
func TestReplaceConcepts_WholesaleReplaceDropsStaleFileLinks(t *testing.T) {
	s := openTestStore(t)

	// Generation 1: "billing" is linked to two files.
	if err := s.ReplaceConcepts([]ConceptRow{
		{Name: "billing", Description: "invoicing and tax", Content: "body A",
			Files: []string{"internal/billing/invoice.go", "internal/billing/tax.go"}},
		{Name: "shipping", Description: "rate quoting", Content: "body B",
			Files: []string{"internal/shipping/rate.go"}},
	}); err != nil {
		t.Fatalf("ReplaceConcepts gen1: %v", err)
	}

	// Generation 2: the concept set no longer contains "billing".
	if err := s.ReplaceConcepts([]ConceptRow{
		{Name: "shipping", Description: "rate quoting", Content: "body B",
			Files: []string{"internal/shipping/rate.go"}},
	}); err != nil {
		t.Fatalf("ReplaceConcepts gen2: %v", err)
	}

	if _, ok, err := s.GetConcept("billing"); err != nil || ok {
		t.Fatalf("GetConcept(billing) after drop: ok=%v err=%v, want ok=false", ok, err)
	}
	surviving, ok, err := s.GetConcept("shipping")
	if err != nil || !ok {
		t.Fatalf("GetConcept(shipping): ok=%v err=%v", ok, err)
	}
	if !equalStrings(surviving.Files, []string{"internal/shipping/rate.go"}) {
		t.Errorf("surviving concept lost its links: Files = %v", surviving.Files)
	}

	// Generation 3: "billing" comes back with a single, different file. If the
	// gen-1 links were left orphaned in the store, they resurface here.
	if err := s.ReplaceConcepts([]ConceptRow{
		{Name: "billing", Description: "invoicing only", Content: "body A2",
			Files: []string{"internal/billing/ledger.go"}},
	}); err != nil {
		t.Fatalf("ReplaceConcepts gen3: %v", err)
	}
	got, ok, err := s.GetConcept("billing")
	if err != nil || !ok {
		t.Fatalf("GetConcept(billing) gen3: ok=%v err=%v", ok, err)
	}
	if !equalStrings(got.Files, []string{"internal/billing/ledger.go"}) {
		t.Errorf("stale file links survived the wholesale replace: Files = %v, want [internal/billing/ledger.go]", got.Files)
	}
}
