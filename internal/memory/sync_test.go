package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRelChunkPath verifies the git-pathspec form of a chunk path: always
// forward-slashed, and naming the same file ChunkPath would write to disk, so
// `git show HEAD:<path>` finds what ChunkPath's caller just wrote.
func TestRelChunkPath(t *testing.T) {
	const author = "Alice Example <alice@example.com>"
	got := RelChunkPath(author)
	want := ".tu-agent/share/memory/chunks/chunk-" + authorSlug(author) + ".jsonl.gz"
	if got != want {
		t.Fatalf("RelChunkPath(%q) = %q, want %q", author, got, want)
	}
	if filepath.Base(ChunkPath(t.TempDir(), author)) != filepath.Base(got) {
		t.Fatalf("RelChunkPath and ChunkPath must agree on the filename")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src := openTestStoreInternal(t)
	if _, err := src.Upsert("decision/rag", "use graph", UpsertOpts{Author: "alice", Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	recs, err := src.ExportRecords("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 exported record, got %d", len(recs))
	}

	dst := openTestStoreInternal(t)
	res, err := dst.ImportRecords(recs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 || res.Updated != 0 || res.Skipped != 0 {
		t.Fatalf("import result = %+v, want 1 inserted", res)
	}
	got, _, err := dst.Search("graph", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "use graph" || got[0].SyncID != recs[0].SyncID {
		t.Fatalf("imported observation mismatch: %+v", got)
	}
}

func TestImportMaxRevisionWins(t *testing.T) {
	src := openTestStoreInternal(t)
	if _, err := src.Upsert("decision/rag", "v1", UpsertOpts{Author: "alice"}); err != nil {
		t.Fatal(err)
	}
	recsV1, _ := src.ExportRecords("alice")
	if _, err := src.Upsert("decision/rag", "v2", UpsertOpts{Author: "alice"}); err != nil {
		t.Fatal(err)
	}
	recsV2, _ := src.ExportRecords("alice")

	dst := openTestStoreInternal(t)
	if _, err := dst.ImportRecords(recsV2); err != nil {
		t.Fatal(err)
	}
	// Importing the older revision must not overwrite the newer one.
	res, err := dst.ImportRecords(recsV1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Fatalf("older revision should be skipped, got %+v", res)
	}
	got, _, _ := dst.Search("decision", "", 0)
	if len(got) != 1 || got[0].Content != "v2" {
		t.Fatalf("want v2 to survive, got %+v", got)
	}
}

func TestImportMalformedTimestampFallsBackToNow(t *testing.T) {
	before := time.Now().UTC()

	s := openTestStoreInternal(t)
	rec := ChunkRecord{
		SyncID:    "obs-malformed-ts-test",
		TopicKey:  "arch/malformed",
		Scope:     "project",
		Content:   "content with malformed timestamp",
		Type:      "decision",
		Author:    "bob",
		Revision:  1,
		CreatedAt: "not-a-timestamp",
		UpdatedAt: "2026-01-15T10:00:00Z",
	}
	res, err := s.ImportRecords([]ChunkRecord{rec})
	if err != nil {
		t.Fatalf("ImportRecords with malformed CreatedAt must not error: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("want 1 inserted, got %+v", res)
	}

	// Observation must be searchable — content is the valuable part.
	got, _, err := s.Search("malformed timestamp", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %d", len(got))
	}

	// CreatedAt must NOT be zero time (year 0001) — it fell back to ~now.
	obs := got[0]
	if obs.CreatedAt.Year() <= 2000 {
		t.Fatalf("CreatedAt fell back to zero time %v; want fallback to current time", obs.CreatedAt)
	}
	after := time.Now().UTC()
	if obs.CreatedAt.Before(before) || obs.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v not in expected window [%v, %v]", obs.CreatedAt, before, after)
	}
}

func TestWriteChunkIdempotentAndDeterministic(t *testing.T) {
	dir := t.TempDir()
	recs := []ChunkRecord{
		{SyncID: "obs-b", TopicKey: "b", Scope: "project", Content: "B", Revision: 1, Author: "alice", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		{SyncID: "obs-a", TopicKey: "a", Scope: "project", Content: "A", Revision: 1, Author: "alice", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
	}
	path, written, err := WriteChunk(dir, "alice", recs)
	if err != nil || !written {
		t.Fatalf("first write: written=%v err=%v", written, err)
	}
	first, _ := os.ReadFile(path)

	_, written2, err := WriteChunk(dir, "alice", recs)
	if err != nil {
		t.Fatal(err)
	}
	if written2 {
		t.Fatal("re-writing identical content must be a no-op")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatal("chunk bytes must be deterministic across writes")
	}

	back, err := ReadAllChunks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].SyncID != "obs-a" {
		t.Fatalf("read-back must be sorted by sync_id: %+v", back)
	}
	if filepath.Base(path) != "chunk-alice.jsonl.gz" {
		t.Fatalf("unexpected chunk filename: %s", filepath.Base(path))
	}
}

func TestImportRecordsDoesNotValidateType(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rec := ChunkRecord{
		SyncID: "obs-importtest", TopicKey: "legacy/x", Scope: "project",
		Title: "legacy", Content: "body", Type: "legacy-freeform-type",
		Revision: 1, Author: "someone",
	}
	if _, err := s.ImportRecords([]ChunkRecord{rec}); err != nil {
		t.Fatalf("ImportRecords must not reject a non-canonical type: %v", err)
	}
	got, _, err := s.Search("legacy", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "legacy-freeform-type" {
		t.Fatalf("imported record should keep its type untouched, got %+v", got)
	}
}
