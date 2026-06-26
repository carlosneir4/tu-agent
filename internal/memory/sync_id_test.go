package memory

import (
	"path/filepath"
	"testing"
)

func openTestStoreInternal(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestComputeSyncIDDeterministic(t *testing.T) {
	a := computeSyncID("project", "architecture/auth")
	b := computeSyncID("project", "architecture/auth")
	if a != b {
		t.Fatalf("same inputs must yield same sync_id: %q != %q", a, b)
	}
	if a == computeSyncID("project", "architecture/db") {
		t.Fatal("different topic keys must yield different sync_id")
	}
	if a == computeSyncID("user", "architecture/auth") {
		t.Fatal("different scope must yield different sync_id")
	}
	if len(a) == 0 || a[:4] != "obs-" {
		t.Fatalf("sync_id must be prefixed obs-: %q", a)
	}
}

func TestUpsertAndAddPopulateSyncID(t *testing.T) {
	s := openTestStoreInternal(t)
	up, err := s.Upsert("architecture/auth", "body", UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if up.SyncID != computeSyncID("project", "architecture/auth") {
		t.Fatalf("upsert sync_id mismatch: %q", up.SyncID)
	}
	ad, err := s.Add("note", "scratch", "cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(ad.SyncID) < 4 || ad.SyncID[:4] != "obs-" {
		t.Fatalf("add must set a random obs- sync_id: %q", ad.SyncID)
	}
	if ad.SyncID == up.SyncID {
		t.Fatal("keyless add must not collide with keyed upsert")
	}
}

func TestBackfillSyncIDs(t *testing.T) {
	s := openTestStoreInternal(t)
	if _, err := s.Upsert("decision/x", "body", UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	// Simulate a legacy row: clear sync_id and the backfill flag.
	if _, err := s.db.Exec(`UPDATE observations SET sync_id = ''`); err != nil {
		t.Fatal(err)
	}
	if err := s.setMeta("sync_ids_backfilled", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.backfillSyncIDs(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if o.SyncID == "" {
			t.Fatalf("row %q left without sync_id after backfill", o.TopicKey)
		}
		if o.TopicKey != "" && o.SyncID != computeSyncID(o.Scope, o.TopicKey) {
			t.Fatalf("keyed row %q backfilled with wrong sync_id %q", o.TopicKey, o.SyncID)
		}
	}
}
