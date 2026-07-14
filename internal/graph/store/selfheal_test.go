package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Feature: store-selfheal-schema-exec (graph-robustness-self-heal-single-flight).
//
// The graph database is derived data: if executing the schema against an
// existing graph.db fails (e.g. "file is not a database" on a truncated or
// garbage file), store.Open must treat it as corruption — close, delete
// graph.db plus its -wal/-shm sidecars, and retry exactly once via the same
// `attempt` rebuild loop already used for version mismatches (store.go:70-73).
//
// sql.Open for sqlite3 is lazy: a garbage graph.db only surfaces as an error
// at db.Exec(schema), which is the code path these tests exercise.

// @s1: garbage graph.db self-heals into a working fresh store.
func TestOpenSelfHealsGarbageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	if err := os.WriteFile(path, []byte("not a sqlite database, just arbitrary garbage bytes"), 0o644); err != nil {
		t.Fatalf("writing garbage graph.db: %v", err)
	}

	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open on corrupt graph.db should self-heal into a fresh store, got error: %v", err)
	}
	defer s.Close()

	ev, err := s.Meta("extractor_version")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if ev != "v1" {
		t.Errorf("extractor_version = %q, want v1", ev)
	}

	n, err := s.FileCount()
	if err != nil {
		t.Fatalf("FileCount: %v", err)
	}
	if n != 0 {
		t.Errorf("FileCount after self-heal = %d, want 0 (fresh store)", n)
	}
}

// @s2: self-heal removes stale -wal and -shm sidecars beside a garbage graph.db.
func TestOpenSelfHealRemovesSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	walPath, shmPath := path+"-wal", path+"-shm"

	if err := os.WriteFile(path, []byte("garbage, not a real sqlite file"), 0o644); err != nil {
		t.Fatalf("writing garbage graph.db: %v", err)
	}
	if err := os.WriteFile(walPath, []byte("stale-wal"), 0o644); err != nil {
		t.Fatalf("writing stale wal sidecar: %v", err)
	}
	if err := os.WriteFile(shmPath, []byte("stale-shm"), 0o644); err != nil {
		t.Fatalf("writing stale shm sidecar: %v", err)
	}

	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open on corrupt graph.db with stale sidecars should self-heal, got error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(walPath); !os.IsNotExist(err) {
		t.Errorf("wal sidecar still present after self-heal: err=%v", err)
	}
	if _, err := os.Stat(shmPath); !os.IsNotExist(err) {
		t.Errorf("shm sidecar still present after self-heal: err=%v", err)
	}
}

// @s3: rebuild that fails again returns a wrapped error instead of looping.
//
// Injection: graph.db is a non-empty DIRECTORY. sql.Open/Exec against it fails
// (it is not a valid sqlite file), so self-heal is triggered; but os.Remove on
// a non-empty directory also fails (ENOTEMPTY), so delete-and-recreate cannot
// repair it. Open must still return promptly with a non-nil, wrapped error —
// never loop forever. This also works when running as root (no permission
// bits involved), unlike a read-only-parent-dir injection.
func TestOpenSelfHealGivesUpAfterOneRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("creating directory at graph.db path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("populating directory so os.Remove fails (ENOTEMPTY): %v", err)
	}

	type result struct {
		s   *Store
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := Open(path, "v1")
		done <- result{s, err}
	}()

	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("store.Open did not return within 5s — self-heal must retry once and give up, not loop forever")
	}

	if r.s != nil {
		r.s.Close()
	}
	if r.err == nil {
		t.Fatal("Open on unrepairable corruption returned no error, want a non-nil wrapped error")
	}
	if !strings.Contains(r.err.Error(), "graph.Store.Open") {
		t.Errorf("error = %q, want it to wrap with the graph.Store.Open prefix", r.err.Error())
	}
	// The raw schema-exec failure must not be surfaced verbatim: a self-heal
	// implementation intercepts it, attempts delete-and-recreate, and — when
	// that repair itself fails — returns an error describing THAT failure
	// (e.g. removing the corrupt db), not the original "applying schema"
	// message. Today's code has no self-heal branch at all and returns the
	// raw schema-exec error directly, so this assertion is the one that
	// currently fails.
	if strings.Contains(r.err.Error(), "applying schema") {
		t.Errorf("error = %q, still the raw schema-exec error — self-heal was not attempted", r.err.Error())
	}
}

// @s4: give-up after the retry itself fails again must wrap the SECOND
// attempt's underlying cause, not discard it behind a bare message.
//
// Injection: graph.db lives inside a subdirectory that is never created.
// SQLite cannot open/create a file whose parent directory does not exist, so
// db.Exec(schema) fails identically on every attempt — this is deterministic
// and race-free, unlike trying to make corruption reappear after a real
// delete. On attempt 0, os.Remove(path) (and its -wal/-shm sidecars) return
// ENOENT, which rebuildOrGiveUp already treats as success (os.IsNotExist),
// so Open loops to attempt 1. There, schema-exec fails again for the exact
// same reason, driving rebuildOrGiveUp into its `attempt > 0` branch with a
// genuine, distinct second-attempt cause available in Open's scope. Today
// that branch returns a bare fmt.Errorf with no %w, so the cause never
// reaches the caller — CLAUDE.md forbids swallowing errors, and the spec's
// Contract item 1 requires it wrapped.
func TestOpenGiveUpWrapsSecondAttemptCause(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-subdir", "graph.db")

	_, err := Open(path, "v1")
	if err == nil {
		t.Fatal("Open on a path whose parent directory never exists should fail on both attempts, got no error")
	}
	if err.Error() == "graph.Store.Open: rebuild persists after retry" {
		t.Errorf("error = %q, give-up path returns the bare retry message and discards the second attempt's underlying cause instead of wrapping it with %%w", err.Error())
	}
}

// Feature: store-selfheal-meta-error (graph-robustness-self-heal-single-flight).
//
// store.Open currently discards the error from Meta("schema_version") and
// Meta("extractor_version") with `sv, _ :=` / `ev, _ :=`. Meta already
// normalizes sql.ErrNoRows to ("", nil), so a real (non-ErrNoRows) read
// failure is the only way these can return a non-nil error — and today that
// error is silently thrown away, so the (sv=="" && ev=="") "fresh" check
// fires on a possibly-corrupt database and Open keeps using it.
//
// Deterministic injection: pre-create a VALID SQLite file at the store path
// whose metadata table exists but lacks the "value" column. The schema's
// `CREATE TABLE IF NOT EXISTS metadata (...)` in Open is then a no-op (the
// table already exists), so `SELECT value FROM metadata WHERE key = ?`
// fails with a genuine driver error (no such column: value) — not
// sql.ErrNoRows.

// createCrippledMetadataDB pre-creates a valid sqlite3 file at path whose
// metadata table has no "value" column, so any later `SELECT value FROM
// metadata` fails with a real (non-ErrNoRows) error. When seedFile is true, a
// files table matching the production schema's columns is also created and
// seeded with one row, so a store that (incorrectly) keeps operating on this
// database — instead of discarding it and rebuilding — would report a
// nonzero FileCount.
func createCrippledMetadataDB(t *testing.T, path string, seedFile bool) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatalf("opening fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE metadata (key TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("creating crippled metadata table: %v", err)
	}
	if seedFile {
		if _, err := db.Exec(`CREATE TABLE files (
			path TEXT PRIMARY KEY, sha256 TEXT NOT NULL, language TEXT,
			status TEXT NOT NULL DEFAULT 'ok', package TEXT DEFAULT '',
			imports TEXT DEFAULT '[]', size INT DEFAULT 0, mtime_ns INT DEFAULT 0,
			parsed_at TEXT)`); err != nil {
			t.Fatalf("creating fixture files table: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO files(path,sha256,language,status,package,imports,size,mtime_ns,parsed_at)
			VALUES(?,?,?,?,?,?,?,?,datetime('now'))`,
			"pre-existing.go", "deadbeef", "go", "ok", "", "[]", 0, 0); err != nil {
			t.Fatalf("seeding fixture files row: %v", err)
		}
	}
}

// @s1: a metadata read error (crippled "value" column) must trigger the same
// rebuild-once self-heal path as a schema-exec failure, producing a working
// fresh store rather than an error or a suspect one.
func TestOpenSelfHealsOnMetaReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	createCrippledMetadataDB(t, path, false)

	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open on a graph.db with unreadable metadata should self-heal into a fresh store, got error: %v", err)
	}
	defer s.Close()

	ev, err := s.Meta("extractor_version")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if ev != "v1" {
		t.Errorf("extractor_version = %q, want v1", ev)
	}
}

// @s2: the suspect database (metadata unreadable) must be discarded, not
// reused — a pre-seeded files row from before the self-heal must not survive
// into the returned store.
func TestOpenDiscardsSuspectDBOnMetaReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	createCrippledMetadataDB(t, path, true)

	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open on a graph.db with unreadable metadata should self-heal into a fresh store, got error: %v", err)
	}
	defer s.Close()

	n, err := s.FileCount()
	if err != nil {
		t.Fatalf("FileCount: %v", err)
	}
	if n != 0 {
		t.Errorf("FileCount after self-heal = %d, want 0 — the pre-existing suspect database (and its rows) must be discarded, not reused", n)
	}
}

// @s3: a fresh, nonexistent database still opens normally without any
// rebuild — regression pin for the ErrNoRows path (Meta("...") returning
// ("", nil) on a brand-new metadata table is NOT an error and must not
// trigger self-heal).
func TestOpenFreshDatabaseStillOpensWithoutRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")

	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open on a fresh path should succeed without error, got: %v", err)
	}
	defer s.Close()

	sv, err := s.Meta("schema_version")
	if err != nil {
		t.Fatalf("Meta(schema_version): %v", err)
	}
	if sv != schemaVersion {
		t.Errorf("schema_version = %q, want %q", sv, schemaVersion)
	}
	ev, err := s.Meta("extractor_version")
	if err != nil {
		t.Fatalf("Meta(extractor_version): %v", err)
	}
	if ev != "v1" {
		t.Errorf("extractor_version = %q, want v1", ev)
	}
}
