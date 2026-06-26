package memory

import (
	"path/filepath"
	"testing"
)

func TestOpen_FallbackWithoutFTS(t *testing.T) {
	ftsDisabled = true
	t.Cleanup(func() { ftsDisabled = false })
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.FTSEnabled() {
		t.Error("FTSEnabled = true, want false when the module probe is disabled")
	}
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := s.Search("jwt", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("LIKE fallback results = %d, want 1", len(got))
	}
}

func TestOpen_RebuildIndexesExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !s.FTSEnabled() {
		s.Close()
		t.Skip("binary built without -tags sqlite_fts5")
	}
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	var n int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&n); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if n != 1 {
		t.Errorf("observations_fts rows = %d, want 1 (rebuild on open)", n)
	}
}

func TestOpen_FTSBumpsSchemaVersion(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}
	v, err := s.meta("schema_version")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if v != "2" {
		t.Errorf("schema_version = %q, want 2", v)
	}
}

// openFTSStoreInternal opens a fresh store and skips unless FTS is active.
func openFTSStoreInternal(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}
	return s
}

func TestAdd_IndexesIntoFTS(t *testing.T) {
	s := openFTSStoreInternal(t)
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("observations_fts rows = %d, want 1 (indexed without reopen)", n)
	}
}

func TestUpsert_ReindexesFTS(t *testing.T) {
	s := openFTSStoreInternal(t)
	obs, err := s.Upsert("arch/queue", "messages flow through the broker", UpsertOpts{})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if _, err := s.Upsert("arch/queue", "events flow through the dispatcher", UpsertOpts{}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, obs.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("index rows for id = %d, want 1 (delete + reinsert, no duplicates)", n)
	}
	var content string
	if err := s.db.QueryRow(`SELECT content FROM observations_fts WHERE id = ?`, obs.ID).Scan(&content); err != nil {
		t.Fatalf("select content: %v", err)
	}
	if content != "events flow through the dispatcher" {
		t.Errorf("indexed content = %q, want the revised content", content)
	}
	var stale int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE content = 'messages flow through the broker'`).Scan(&stale); err != nil {
		t.Fatalf("stale count: %v", err)
	}
	if stale != 0 {
		t.Errorf("old content still in index: want 0 rows, got %d", stale)
	}
}
