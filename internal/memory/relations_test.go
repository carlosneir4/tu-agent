package memory

import (
	"path/filepath"
	"testing"
)

func openRelStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRelateAndQuery(t *testing.T) {
	s := openRelStore(t)
	obs, err := s.Upsert("architecture/cache", "cache is fragile", UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	node := "internal/billing/invoice.go::InvoiceService"
	if _, err := s.Relate(obs.ID, node, "related"); err != nil {
		t.Fatalf("Relate: %v", err)
	}
	// idempotent
	if _, err := s.Relate(obs.ID, node, "related"); err != nil {
		t.Fatalf("Relate again: %v", err)
	}
	rels, err := s.RelationsTo([]string{node})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].FromID != obs.ID || rels[0].Type != "related" {
		t.Fatalf("RelationsTo = %+v", rels)
	}
	// resolve the observation end
	got, err := s.ObservationsByID([]string{rels[0].FromID, node})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != obs.ID {
		t.Fatalf("ObservationsByID should resolve only the observation, got %+v", got)
	}
}

func TestRelateRejectsUnknownType(t *testing.T) {
	s := openRelStore(t)
	if _, err := s.Relate("a", "b", "bogus"); err == nil {
		t.Error("unknown relation type must be rejected")
	}
}

func TestRelationsEmptyIDs(t *testing.T) {
	s := openRelStore(t)
	if rels, err := s.RelationsTo(nil); err != nil || rels != nil {
		t.Errorf("empty ids → nil, nil; got %v, %v", rels, err)
	}
}

// TestObservationsByIDDeterministicOrder pins the ORDER BY topic_key, id so the
// "Related knowledge" section renders the same order across runs.
func TestObservationsByIDDeterministicOrder(t *testing.T) {
	s := openRelStore(t)
	bb, err := s.Upsert("bbb/topic", "second", UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	aa, err := s.Upsert("aaa/topic", "first", UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	// Query with the bb id first; result must still come back topic-key ordered (aaa before bbb).
	got, err := s.ObservationsByID([]string{bb.ID, aa.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TopicKey != "aaa/topic" || got[1].TopicKey != "bbb/topic" {
		t.Fatalf("ObservationsByID order = %+v, want aaa/topic then bbb/topic", got)
	}
}

func TestDeleteRelationsByType(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Relate("obs1", "nodeA", "documents_auto"); err != nil {
		t.Fatalf("relate auto: %v", err)
	}
	if _, err := s.Relate("obs1", "nodeB", "documents"); err != nil {
		t.Fatalf("relate manual: %v", err)
	}

	n, err := s.DeleteRelationsByType("obs1", "documents_auto")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}
	// Manual relation survives.
	rels, err := s.RelationsFrom([]string{"obs1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].Type != "documents" {
		t.Fatalf("survivors = %+v, want one 'documents'", rels)
	}
}
