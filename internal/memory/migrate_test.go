package memory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

const legacyJSON = `[
  {"id":"100","timestamp":"2026-01-05T10:00:00Z","topic":"auth","content":"JWT expiry 1h","source":"agent"},
  {"id":"101","timestamp":"2026-01-06T11:00:00Z","topic":"db","content":"pool size 10","source":"human"}
]`

func TestOpen_MigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "memory.json")
	if err := os.WriteFile(jsonPath, []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("Open with legacy json: %v", err)
	}
	defer s.Close()

	obs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("imported %d observations, want 2", len(obs))
	}
	// List is newest-first: db (2026-01-06) then auth (2026-01-05).
	if obs[0].Title != "db" || obs[1].Title != "auth" {
		t.Errorf("titles = [%s %s], want [db auth]", obs[0].Title, obs[1].Title)
	}
	for _, o := range obs {
		if o.TopicKey != "" {
			t.Errorf("migrated row %s has topic_key %q, want empty", o.ID, o.TopicKey)
		}
		if o.Revision != 1 {
			t.Errorf("migrated row %s revision = %d, want 1", o.ID, o.Revision)
		}
	}
	if got := obs[1].CreatedAt.Format("2006-01-02"); got != "2026-01-05" {
		t.Errorf("original timestamp not preserved: %s", got)
	}

	// The JSON file is never modified — it is the backup.
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != legacyJSON {
		t.Error("memory.json was modified during migration")
	}
}

func TestOpen_MigrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "memory.db")

	s, err := memory.Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := memory.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	n, err := s2.Len()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("Len after second Open = %d, want 2 (no re-import)", n)
	}
}

func TestOpen_CorruptLegacyJSONFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Open(filepath.Join(dir, "memory.db")); err == nil {
		t.Fatal("expected Open to fail on corrupt memory.json")
	}
}

func TestOpen_NoLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("Open with no memory.json: %v", err)
	}
	defer s.Close()
	n, err := s.Len()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("Len = %d, want 0 (no JSON to import)", n)
	}
}

func TestOpen_EmptyLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "memory.db")

	s, err := memory.Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if n, err := s.Len(); err != nil || n != 0 {
		t.Errorf("Len after empty-array import: n=%d err=%v, want 0 nil", n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Second open must not re-import (idempotency for the empty-array case).
	s2, err := memory.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	if n, err := s2.Len(); err != nil || n != 0 {
		t.Errorf("Len after second Open: n=%d err=%v, want 0 nil", n, err)
	}
}

func TestOpen_MigratesTimestampToBothColumns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if !o.CreatedAt.Equal(o.UpdatedAt) {
			t.Errorf("row %s: CreatedAt %v != UpdatedAt %v (both must carry original timestamp)",
				o.ID, o.CreatedAt, o.UpdatedAt)
		}
	}
}
