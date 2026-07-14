package memory_test

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// openFTSStore opens a fresh store and skips unless FTS5 is compiled in.
func openFTSStore(t *testing.T) *memory.Store {
	t.Helper()
	s := openTestStore(t)
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}
	return s
}

func TestSearch_FTSRanksByRelevance(t *testing.T) {
	s := openFTSStore(t)
	mustAdd(t, s, "auth-tokens", "tokens tokens tokens: rotation, signing, and expiry of tokens")
	mustAdd(t, s, "deploy-secrets", "the vault holds one deploy credential and some tokens")
	got, _, err := s.Search("tokens", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d, want 2", len(got))
	}
	if got[0].Title != "auth-tokens" {
		t.Errorf("got[0].Title = %q, want auth-tokens (more term hits ranks first)", got[0].Title)
	}
}

func TestSearch_FTSSpansMultipleObservations(t *testing.T) {
	s := openFTSStore(t)
	mustAdd(t, s, "auth", "JWT rotation is handled by the auth service")
	mustAdd(t, s, "deploy", "the pipeline promotes builds to staging")
	mustAdd(t, s, "storage", "the cache evicts entries after ten minutes")
	got, _, err := s.Search("jwt pipeline cache", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("results = %d, want 3 (terms are OR-joined)", len(got))
	}
}

func TestSearch_FTSUnrelatedQueryReturnsNothing(t *testing.T) {
	s := openFTSStore(t)
	mustAdd(t, s, "auth", "JWT rotation is handled by the auth service")
	got, _, err := s.Search("zeppelin", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("results = %d, want 0", len(got))
	}
}

func TestSearch_FTSReflectsUpsertRevision(t *testing.T) {
	s := openFTSStore(t)
	if _, err := s.Upsert("arch/queue", "messages flow through the broker", memory.UpsertOpts{}); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if _, err := s.Upsert("arch/queue", "events flow through the dispatcher", memory.UpsertOpts{}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, _, err := s.Search("dispatcher", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("dispatcher results = %d, want 1", len(got))
	}
	stale, _, err := s.Search("broker", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("broker results = %d, want 0 (old revision must leave the index)", len(stale))
	}
}

func TestSearch_FTSQuotesOperatorSyntax(t *testing.T) {
	s := openFTSStore(t)
	mustAdd(t, s, "auth", "JWT rotation")
	if _, _, err := s.Search(`AND NOT "broken`, "", 0); err != nil {
		t.Errorf("Search with operator-looking input errored: %v, want graceful handling", err)
	}
}

func TestSearch_EmptyQueryReturnsAll(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "a", "one")
	mustAdd(t, s, "b", "two")
	got, _, err := s.Search("", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("results = %d, want 2 (empty query keeps listing everything)", len(got))
	}
}
