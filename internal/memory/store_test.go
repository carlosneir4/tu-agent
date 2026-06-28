package memory_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

// TestOpenMigratesLegacyDBWithoutSyncID guards the in-place migration of a
// database created by a pre-sync_id binary (observations table has author but
// no sync_id column). Opening it must add the column and backfill, not fail
// with "no such column: sync_id". The sqlite3 driver is registered via the
// memory package's blank import.
func TestOpenMigratesLegacyDBWithoutSyncID(t *testing.T) {
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
	if _, err := raw.Exec(`INSERT INTO observations (id, topic_key, scope, content, revision, created_at, updated_at)
		VALUES ('x', 'decision/legacy', 'project', 'body', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(path)
	if err != nil {
		t.Fatalf("Open on legacy DB (no sync_id) failed: %v", err)
	}
	defer s.Close()

	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 {
		t.Fatalf("want 1 migrated observation, got %d", len(obs))
	}
	if obs[0].SyncID == "" {
		t.Fatal("legacy row was not backfilled with a sync_id")
	}
}

func openTestStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func storeLen(t *testing.T, s *memory.Store) int {
	t.Helper()
	n, err := s.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	return n
}

func mustAdd(t *testing.T, s *memory.Store, topic, content string) {
	t.Helper()
	if _, err := s.Add(topic, content, "agent"); err != nil {
		t.Fatalf("Add(%q): %v", topic, err)
	}
}

func TestOpen_CreatesEmptyStore(t *testing.T) {
	s := openTestStore(t)
	if n := storeLen(t, s); n != 0 {
		t.Errorf("Len = %d, want 0", n)
	}
}

func TestAdd_PersistsImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := memory.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Add("auth", "JWT tokens expire in 1h", "agent"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := memory.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if n := storeLen(t, s2); n != 1 {
		t.Errorf("Len after reopen = %d, want 1", n)
	}
}

func TestAdd_SameTopicDoesNotCollide(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "auth", "first")
	mustAdd(t, s, "auth", "second")
	if n := storeLen(t, s); n != 2 {
		t.Errorf("Len = %d, want 2 (Add never upserts)", n)
	}
}

func TestUpsert_RevisionSemantics(t *testing.T) {
	s := openTestStore(t)

	first, err := s.Upsert("architecture/auth", "v1 content", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if first.Revision != 1 {
		t.Errorf("first Revision = %d, want 1", first.Revision)
	}

	same, err := s.Upsert("architecture/auth", "v1 content", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("identical Upsert: %v", err)
	}
	if same.Revision != 1 {
		t.Errorf("identical content Revision = %d, want 1 (no bump)", same.Revision)
	}
	if same.ID != first.ID {
		t.Errorf("identical Upsert created a new row: %s != %s", same.ID, first.ID)
	}

	second, err := s.Upsert("architecture/auth", "v2 content", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("changed Upsert: %v", err)
	}
	if second.Revision != 2 {
		t.Errorf("changed content Revision = %d, want 2", second.Revision)
	}
	if second.ID != first.ID {
		t.Error("Upsert created a duplicate row")
	}
	if n := storeLen(t, s); n != 1 {
		t.Errorf("Len = %d, want 1 (upsert must not duplicate)", n)
	}
}

func TestUpsert_EmptyKeyRejected(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("", "content", memory.UpsertOpts{}); err == nil {
		t.Fatal("expected error for empty topic key")
	}
}

func TestUpsert_DifferentScopesDoNotCollide(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("k", "a", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	obs, err := s.Upsert("k", "b", memory.UpsertOpts{Scope: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Revision != 1 {
		t.Errorf("personal-scope Revision = %d, want 1 (separate row)", obs.Revision)
	}
	if n := storeLen(t, s); n != 2 {
		t.Errorf("Len = %d, want 2", n)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "auth", "JWT tokens expire in 1h")
	mustAdd(t, s, "db", "Postgres pool size 10")

	results, err := s.Search("jwt", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Title != "auth" {
		t.Errorf("Search(jwt) = %+v, want one result with Title auth", results)
	}

	none, err := s.Search("kubernetes", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("Search(kubernetes) returned %d results, want 0", len(none))
	}
}

func TestSearch_MatchesTopicKey(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("architecture/caching", "ttl is 60s", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	results, err := s.Search("caching", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search(caching) = %d results, want 1 (topic_key match)", len(results))
	}
}

func TestRecent_OldestFirst(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "a", "content a")
	mustAdd(t, s, "b", "content b")
	mustAdd(t, s, "c", "content c")

	got, err := s.Recent(2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 2 || got[0].Title != "b" || got[1].Title != "c" {
		t.Errorf("Recent(2) titles = %v, want [b c] (oldest of the recent first)", titles(got))
	}

	all, err := s.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Recent(10) = %d results, want all 3", len(all))
	}
}

func titles(obs []memory.Observation) []string {
	out := make([]string, 0, len(obs))
	for _, o := range obs {
		out = append(out, o.Title)
	}
	return out
}

func TestList_NewestFirst(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "old", "first")
	mustAdd(t, s, "new", "second")

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Title != "new" {
		t.Errorf("List() titles = %v, want newest first", titles(got))
	}
}

func TestUpsertStoresAuthor(t *testing.T) {
	s := openTestStore(t)
	obs, err := s.Upsert("arch/auth", "JWT lives in the gateway", memory.UpsertOpts{Author: "dev@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Author != "dev@example.com" {
		t.Errorf("Author = %q, want dev@example.com", obs.Author)
	}
	// Upsert with a new author updates it; empty author preserves the old one.
	obs2, err := s.Upsert("arch/auth", "JWT lives in the gateway v2", memory.UpsertOpts{Author: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if obs2.Author != "other@example.com" {
		t.Errorf("Author after update = %q, want other@example.com", obs2.Author)
	}
	obs3, err := s.Upsert("arch/auth", "v3", memory.UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if obs3.Author != "other@example.com" {
		t.Errorf("Author after empty-author update = %q, want preserved other@example.com", obs3.Author)
	}
}

func TestOpenMigratesAuthorColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.db")
	s, err := memory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("k", "v", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := memory.Open(path) // reopen: ALTER must be idempotent
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
}

func TestSearchTypeFilter(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Upsert("trap/cache", "cache invalidation race", memory.UpsertOpts{Type: "gotcha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("design/cache", "cache layer decision", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Search("cache", "gotcha") // keyword + type → only the gotcha
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "gotcha" {
		t.Fatalf("Search(cache, gotcha) = %+v, want only the gotcha", got)
	}

	got, err = s.Search("cache", "") // no type filter → both (today's behavior)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Search(cache, \"\") = %d, want 2", len(got))
	}

	got, err = s.Search("", "decision") // empty query + type → list all of that type
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "decision" {
		t.Fatalf("Search(\"\", decision) = %+v, want the one decision", got)
	}
}

func TestUpsertValidatesType(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, ty := range []string{"", "bug-pattern", "decision", "architecture", "testing", "reference", "gotcha"} {
		if _, err := s.Upsert("k/"+ty, "body", memory.UpsertOpts{Type: ty}); err != nil {
			t.Errorf("Upsert with valid type %q errored: %v", ty, err)
		}
	}
	_, err = s.Upsert("k/bad", "body", memory.UpsertOpts{Type: "nonsense"})
	if err == nil {
		t.Fatal("Upsert with invalid type should error")
	}
	if !strings.Contains(err.Error(), "invalid observation type") || !strings.Contains(err.Error(), "gotcha") {
		t.Errorf("error should name the problem and list valids, got: %v", err)
	}
}

func TestUpsert_SkillTypeValidAndDerived(t *testing.T) {
	dir := t.TempDir()
	s, err := memory.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Explicit type "skill" is accepted.
	o, err := s.Upsert("skill/checkout", "body", memory.UpsertOpts{Type: "skill"})
	if err != nil {
		t.Fatalf("explicit skill type rejected: %v", err)
	}
	if o.Type != "skill" {
		t.Errorf("want type skill, got %q", o.Type)
	}
	// A skill/ topic with no explicit type derives type "skill".
	o2, err := s.Upsert("skill/payment", "body", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if o2.Type != "skill" {
		t.Errorf("want derived type skill, got %q", o2.Type)
	}
}

func TestRelationsByType_FiltersByType(t *testing.T) {
	dir := t.TempDir()
	s, err := memory.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	a, _ := s.Upsert("testing/a", "x", memory.UpsertOpts{Type: "testing"})
	b, _ := s.Upsert("decision/b", "y", memory.UpsertOpts{Type: "decision"})
	c, _ := s.Upsert("gotcha/c", "z", memory.UpsertOpts{Type: "gotcha"})
	if _, err := s.Relate(a.ID, b.ID, "conflicts_with"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Relate(b.ID, c.ID, "conflicts_with"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Relate(a.ID, c.ID, "related"); err != nil {
		t.Fatal(err)
	}

	got, err := s.RelationsByType("conflicts_with")
	if err != nil {
		t.Fatalf("RelationsByType: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 conflicts_with, got %d", len(got))
	}
	for _, r := range got {
		if r.Type != "conflicts_with" {
			t.Errorf("got relation of type %q", r.Type)
		}
	}
}
