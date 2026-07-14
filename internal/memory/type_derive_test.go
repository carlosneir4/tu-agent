package memory_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestUpsertDerivesTypeFromTopicKeyPrefix verifies that saving with an empty
// explicit type derives the type from the topic key prefix when that prefix is
// a valid observation type. This is the safety net so notes saved as
// "bug-pattern/foo" without --type are still reachable by --type filtering.
func TestUpsertDerivesTypeFromTopicKeyPrefix(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	obs, err := s.Upsert("bug-pattern/null-slug", "body", memory.UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Type != "bug-pattern" {
		t.Fatalf("expected derived type %q, got %q", "bug-pattern", obs.Type)
	}
}

// TestUpsertDoesNotDeriveTypeFromInvalidPrefix verifies that a topic key prefix
// that is not a valid type (e.g. "project/") leaves the type empty.
func TestUpsertDoesNotDeriveTypeFromInvalidPrefix(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	obs, err := s.Upsert("project/rag-proposal", "body", memory.UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Type != "" {
		t.Fatalf("expected empty type for invalid prefix, got %q", obs.Type)
	}
}

// TestUpsertExplicitTypeWinsOverPrefix verifies that an explicit type is never
// overridden by the topic key prefix.
func TestUpsertExplicitTypeWinsOverPrefix(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	obs, err := s.Upsert("decision/x", "body", memory.UpsertOpts{Type: "gotcha"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Type != "gotcha" {
		t.Fatalf("expected explicit type to win (gotcha), got %q", obs.Type)
	}
}

// TestUpsertDerivedTypeIsSearchable verifies the end-to-end payoff: a note saved
// without an explicit type is found by a type-filtered search.
func TestUpsertDerivedTypeIsSearchable(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Upsert("gotcha/styleguide-generated", "view interfaces are generated", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Search("generated", "gotcha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 gotcha result, got %d", len(got))
	}
}

// TestOpenBackfillsTypeFromTopicKey guards the in-place migration: an existing
// database whose rows carry the type only in the topic key prefix (type column
// empty, as produced by pre-typing binaries) gets its type column backfilled on
// Open, so --type filtering finds those legacy rows.
func TestOpenBackfillsTypeFromTopicKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE observations (
		id TEXT PRIMARY KEY, topic_key TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL DEFAULT 'project',
		project TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT, author TEXT DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	// Two legacy rows with empty type: one with a valid prefix, one invalid.
	if _, err := raw.Exec(`INSERT INTO observations (id, topic_key, scope, content, revision, created_at, updated_at)
		VALUES ('a', 'bug-pattern/jacoco', 'project', 'body', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('b', 'project/rag', 'project', 'body', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(path)
	if err != nil {
		t.Fatalf("Open on legacy DB failed: %v", err)
	}
	defer s.Close()

	got, _, err := s.Search("body", "bug-pattern", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TopicKey != "bug-pattern/jacoco" {
		t.Fatalf("expected backfilled bug-pattern row to be type-searchable, got %+v", got)
	}
	// The invalid-prefix row must remain untyped.
	all, _, err := s.Search("body", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range all {
		if o.TopicKey == "project/rag" && o.Type != "" {
			t.Fatalf("project/ row should stay untyped, got type %q", o.Type)
		}
	}
}
