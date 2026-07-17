// Package memory persists durable observations in SQLite. Unlike the graph
// store, memory is NOT derived data: schema changes must migrate in place,
// never delete the database.
package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const schemaVersion = "1"

// timeFormat is a FIXED-WIDTH RFC3339 with 9 fractional digits. SQLite compares
// these timestamp columns as strings (range filters and ORDER BY ... DESC), so
// the fraction must not vary in width: time.RFC3339Nano trims trailing zeros,
// which makes a later instant sort BEFORE an earlier one lexically (".0945Z" vs
// ".094545Z" → 'Z' > '4'). The zero-padded layout keeps lexical order ==
// chronological order. Parsing stays lenient about fractional width.
//
// Transitional caveat: rows written by an older binary are variable-width, so an
// old-row/new-row pair can still mis-sort until the old row is rewritten. This is
// a strict subset of the pre-fix problem (which mis-ordered every pair) and
// self-heals — any Upsert/SessionEnd/Rescope/ImportRecords rewrites the row.
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// timeParseFormat reads timestamps written by either this version (fixed width)
// or older binaries (time.RFC3339Nano, variable width, possibly no fraction).
// RFC3339Nano is lenient on parse and accepts any fractional width, including
// none, so it round-trips both. Always PARSE with this, never with timeFormat
// (whose zero-padded fraction would reject a no-fraction or short-fraction input).
const timeParseFormat = time.RFC3339Nano

const schema = `
CREATE TABLE IF NOT EXISTS observations (
  id          TEXT PRIMARY KEY,
  topic_key   TEXT NOT NULL DEFAULT '',
  scope       TEXT NOT NULL DEFAULT 'project',
  project     TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL,
  type        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT '',
  revision    INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  author      TEXT NOT NULL DEFAULT '',
  sync_id     TEXT NOT NULL DEFAULT '',
  deleted_at  TEXT);
CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_upsert
  ON observations(topic_key, scope, project)
  WHERE topic_key != '' AND deleted_at IS NULL;
-- idx_obs_sync is created in Open() AFTER the sync_id ALTER TABLE, never here:
-- a database from a pre-sync_id binary has no sync_id column when this schema
-- runs, so indexing it inline would fail with "no such column: sync_id".
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS memory_relations (
  id            TEXT PRIMARY KEY,
  from_id       TEXT NOT NULL,
  to_id         TEXT NOT NULL,
  relation_type TEXT NOT NULL DEFAULT 'related',
  created_at    TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_relations_to   ON memory_relations(to_id);
CREATE INDEX IF NOT EXISTS idx_relations_from ON memory_relations(from_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_relations_uniq
  ON memory_relations(from_id, to_id, relation_type);
CREATE TABLE IF NOT EXISTS sessions (
  id          TEXT PRIMARY KEY,
  project     TEXT NOT NULL DEFAULT '',
  started_at  TEXT NOT NULL,
  ended_at    TEXT,
  summary     TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_sessions_active
  ON sessions(project) WHERE ended_at IS NULL;
`

// Observation is one persisted memory entry.
type Observation struct {
	ID        string
	TopicKey  string
	Scope     string
	Project   string
	Title     string
	Content   string
	Type      string
	Source    string
	Author    string
	SyncID    string
	Revision  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpsertOpts carries the optional fields of Upsert.
type UpsertOpts struct {
	Title  string // defaults to the topic key
	Type   string // overwrites only when non-empty
	Source string // overwrites only when non-empty
	Scope  string // defaults to "project"
	Author string // overwrites only when non-empty
}

// Store wraps the SQLite connection. Memory is durable: Open never deletes
// the database on a version mismatch.
type Store struct {
	db  *sql.DB
	fts bool
	// openDrift is a one-shot, in-memory-only snapshot of the count-drift
	// probe (see countsDrifted), taken once when this Store is opened and
	// consulted (then cleared) by maybeRebuildFTS. It exists to catch drift
	// left by a writer from before the fts_dirty mechanism existed — see
	// maybeRebuildFTS's doc comment for why it is snapshotted at Open instead
	// of recomputed on every search.
	openDrift bool
}

// Open opens (or creates) the memory database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("memory.Open: mkdir: %w", err)
	}
	// _txlock=immediate makes every s.db.Begin() acquire the write lock at BEGIN
	// instead of upgrading a deferred read lock on first write. Not every
	// explicit transaction in this package writes on every path — Rescope,
	// Retopic, Delete, and ImportRecords all have read-only outcomes, and for
	// ImportRecords (the SessionStart hook) the read-only outcome is in fact the
	// steady state: it opens a tx, finds every record already imported, and
	// commits without writing. Paying BEGIN IMMEDIATE's pessimistic cost on
	// those is still the right trade-off store-wide, because the cost is close
	// to nil here: SetMaxOpenConns(1)
	// (below) already serialises all access through this *sql.DB to one
	// connection, and database/sql has no way to request a per-transaction
	// BEGIN IMMEDIATE without a second handle that would break that
	// serialisation. It matters for rebuildFTS: two stores opened concurrently
	// that both try to rebuild would otherwise race to upgrade the same read
	// snapshot and get SQLITE_BUSY_SNAPSHOT immediately, bypassing
	// _busy_timeout. BEGIN IMMEDIATE makes the loser queue on busy_timeout
	// instead of failing.
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory.Open: applying schema: %w", err)
	}
	// Idempotent ALTER TABLE to add the author column to existing databases.
	if _, err := db.Exec(`ALTER TABLE observations ADD COLUMN author TEXT DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("memory.Open: adding author column: %w", err)
		}
	}
	// Idempotent ALTER TABLE to add sync_id to pre-existing databases.
	if _, err := db.Exec(`ALTER TABLE observations ADD COLUMN sync_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("memory.Open: adding sync_id column: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_sync
		ON observations(sync_id) WHERE sync_id != '' AND deleted_at IS NULL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory.Open: creating sync_id index: %w", err)
	}
	// Serialize all writes through a single connection; SQLite does not support
	// concurrent writers and WAL mode does not change that constraint.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	v, err := s.meta("schema_version")
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	if v == "" {
		if err := s.setMeta("schema_version", schemaVersion); err != nil {
			s.Close()
			return nil, fmt.Errorf("memory.Open: %w", err)
		}
	}
	if err := s.migrateFromJSON(path); err != nil {
		s.Close()
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	if err := s.backfillSyncIDs(); err != nil {
		s.Close()
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	if err := s.backfillTypes(); err != nil {
		s.Close()
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	if err := s.initFTS(); err != nil {
		s.Close()
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("memory.Store.Close: %w", err)
	}
	return nil
}

// FTSEnabled reports whether ranked full-text search is active for this
// store (false when the binary was built without -tags sqlite_fts5).
func (s *Store) FTSEnabled() bool { return s.fts }

const obsColumns = `id, topic_key, scope, project, title, content, type, source, revision, created_at, updated_at, author, sync_id`

const insertObsSQL = `INSERT INTO observations
  (id, topic_key, scope, project, title, content, type, source, revision, created_at, updated_at, author, sync_id)
  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`

// Add appends an observation without an upsert key and persists immediately.
func (s *Store) Add(topic, content, source string) (Observation, error) {
	now := time.Now().UTC()
	id, err := newID()
	if err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Add: %w", err)
	}
	syncID, err := randomSyncID()
	if err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Add: %w", err)
	}
	obs := Observation{
		ID: id, Scope: "project", Title: topic, Content: content,
		Source: source, SyncID: syncID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.insert(obs); err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Add: %w", err)
	}
	return obs, nil
}

// Upsert inserts or refines the observation identified by
// (topicKey, scope, project). Changed content bumps the revision; identical
// content only touches updated_at.
func (s *Store) Upsert(topicKey, content string, opts UpsertOpts) (Observation, error) {
	if topicKey == "" {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: topic key cannot be empty")
	}
	if opts.Type != "" && !validObservationTypes[opts.Type] {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: invalid observation type %q (valid: architecture, bug-pattern, decision, gotcha, reference, skill, testing, or empty)", opts.Type)
	}
	// An explicit type always wins; otherwise derive it from the topic key
	// prefix so notes saved as "type/slug" stay reachable by --type filtering.
	effType := opts.Type
	if effType == "" {
		effType = typeFromTopicKey(topicKey)
	}
	scope := opts.Scope
	if scope == "" {
		scope = "project"
	}
	title := opts.Title
	if title == "" {
		title = topicKey
	}
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	existing, found, err := findByKeyTx(tx, topicKey, scope, "")
	if err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", err)
	}
	if !found {
		id, idErr := newID()
		if idErr != nil {
			return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", idErr)
		}
		obs := Observation{
			ID: id, TopicKey: topicKey, Scope: scope, Title: title,
			Content: content, Type: effType, Source: opts.Source,
			Author:   opts.Author,
			SyncID:   computeSyncID(scope, topicKey),
			Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertTx(tx, obs, s.fts); err != nil {
			return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Observation{}, fmt.Errorf("memory.Store.Upsert: commit: %w", err)
		}
		return obs, nil
	}
	if existing.Content == content {
		if _, err := tx.Exec(`UPDATE observations SET updated_at = ? WHERE id = ?`,
			now.Format(timeFormat), existing.ID); err != nil {
			return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Observation{}, fmt.Errorf("memory.Store.Upsert: commit: %w", err)
		}
		existing.UpdatedAt = now
		return existing, nil
	}
	existing.Title = title
	existing.Content = content
	if effType != "" {
		existing.Type = effType
	}
	if opts.Source != "" {
		existing.Source = opts.Source
	}
	if opts.Author != "" {
		existing.Author = opts.Author
	}
	existing.Revision++
	existing.UpdatedAt = now
	if _, err := tx.Exec(`UPDATE observations
		SET title = ?, content = ?, type = ?, source = ?, author = ?, revision = ?, updated_at = ?
		WHERE id = ?`,
		existing.Title, existing.Content, existing.Type, existing.Source, existing.Author,
		existing.Revision, now.Format(timeFormat), existing.ID); err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", err)
	}
	if err := ftsUpdate(tx, s.fts, existing); err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Observation{}, fmt.Errorf("memory.Store.Upsert: commit: %w", err)
	}
	return existing, nil
}

// Search returns live observations matching query. With FTS5 available the
// terms are OR-joined and results are ranked by bm25 relevance; otherwise it
// falls back to case-insensitive substring matching, most recent first.
// Empty queries always take the fallback path (which lists everything).
// typeFilter filters results by type when non-empty. limit caps the number of
// rows returned (limit <= 0 means no cap); the second return is the total
// number of matches before the cap, so a caller can disclose "showing N of M"
// rather than silently truncating.
func (s *Store) Search(query, typeFilter string, limit int) ([]Observation, int, error) {
	if s.fts && strings.TrimSpace(query) != "" {
		return s.searchFTS(query, typeFilter, limit)
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(query))
	q := "%" + esc + "%"
	countSQL := `SELECT count(*) FROM observations
		WHERE deleted_at IS NULL
		  AND (lower(title) LIKE ? ESCAPE '\' OR lower(content) LIKE ? ESCAPE '\' OR lower(topic_key) LIKE ? ESCAPE '\')`
	sql := `SELECT ` + obsColumns + ` FROM observations
		WHERE deleted_at IS NULL
		  AND (lower(title) LIKE ? ESCAPE '\' OR lower(content) LIKE ? ESCAPE '\' OR lower(topic_key) LIKE ? ESCAPE '\')`
	args := []any{q, q, q}
	if typeFilter != "" {
		countSQL += ` AND type = ?`
		sql += ` AND type = ?`
		args = append(args, typeFilter)
	}
	var total int
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("memory.Store.Search: count: %w", err)
	}
	sql += ` ORDER BY updated_at DESC`
	if limit > 0 {
		sql += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("memory.Store.Search: %w", err)
	}
	out, err := collectRows(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("memory.Store.Search: %w", err)
	}
	return out, total, nil
}

// Recent returns the n most recently updated live observations, oldest
// first, so prompt injection reads chronologically ("most recent last").
func (s *Store) Recent(n int) ([]Observation, error) {
	rows, err := s.db.Query(`SELECT `+obsColumns+` FROM observations
		WHERE deleted_at IS NULL ORDER BY updated_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.Recent: %w", err)
	}
	out, err := collectRows(rows)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.Recent: %w", err)
	}
	slices.Reverse(out)
	return out, nil
}

// List returns all live observations, most recently updated first.
func (s *Store) List() ([]Observation, error) {
	rows, err := s.db.Query(`SELECT ` + obsColumns + ` FROM observations
		WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.List: %w", err)
	}
	out, err := collectRows(rows)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.List: %w", err)
	}
	return out, nil
}

// Len returns the number of live observations.
func (s *Store) Len() (int, error) {
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("memory.Store.Len: %w", err)
	}
	return n, nil
}

// execer abstracts *sql.DB and *sql.Tx for insert/query helpers.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Store) insert(o Observation) error {
	return insertTx(s.db, o, s.fts)
}

func insertTx(db execer, o Observation, fts bool) error {
	if _, err := db.Exec(insertObsSQL,
		o.ID, o.TopicKey, o.Scope, o.Project, o.Title, o.Content, o.Type, o.Source,
		o.Revision, o.CreatedAt.Format(timeFormat), o.UpdatedAt.Format(timeFormat), o.Author, o.SyncID); err != nil {
		return fmt.Errorf("memory.Store.insert: %w", err)
	}
	return ftsInsert(db, fts, o)
}

func findByKeyTx(db execer, topicKey, scope, project string) (Observation, bool, error) {
	row := db.QueryRow(`SELECT `+obsColumns+` FROM observations
		WHERE topic_key = ? AND scope = ? AND project = ? AND deleted_at IS NULL`,
		topicKey, scope, project)
	o, err := scanObservation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, false, nil
	}
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.findByKey: %w", err)
	}
	return o, true, nil
}

// meta reads a value from the metadata table; returns "" if the key is absent.
func (s *Store) meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("memory.Store.meta(%s): %w", key, err)
	}
	return v, nil
}

// setMeta writes a key/value pair to the metadata table.
func (s *Store) setMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO metadata(key, value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("memory.Store.setMeta(%s): %w", key, err)
	}
	return nil
}

// Meta reads a metadata value by key ("" if absent). It is the exported form
// of meta, for callers outside the package (e.g. advise's dedup/dismiss
// state) that need to persist a small key/value directly in the store.
func (s *Store) Meta(key string) (string, error) { return s.meta(key) }

// SetMeta writes a metadata key/value pair. It is the exported form of
// setMeta, for callers outside the package (see Meta).
func (s *Store) SetMeta(key, value string) error { return s.setMeta(key, value) }

type rowScanner interface{ Scan(dest ...any) error }

func scanObservation(row rowScanner) (Observation, error) {
	var o Observation
	var created, updated string
	if err := row.Scan(&o.ID, &o.TopicKey, &o.Scope, &o.Project, &o.Title,
		&o.Content, &o.Type, &o.Source, &o.Revision, &created, &updated, &o.Author, &o.SyncID); err != nil {
		return Observation{}, err
	}
	var err error
	if o.CreatedAt, err = time.Parse(timeParseFormat, created); err != nil {
		return Observation{}, fmt.Errorf("parse created_at: %w", err)
	}
	if o.UpdatedAt, err = time.Parse(timeParseFormat, updated); err != nil {
		return Observation{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return o, nil
}

func collectRows(rows *sql.Rows) ([]Observation, error) {
	defer rows.Close()
	var out []Observation
	for rows.Next() {
		o, err := scanObservation(rows)
		if err != nil {
			return nil, fmt.Errorf("memory.collectRows: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// newID returns a random 16-hex-char identifier. An RNG failure is a hard
// error rather than falling back to a weak, predictable/collision-prone
// identifier (e.g. UnixNano) — callers must propagate it.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("memory.newID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// computeSyncID derives a deterministic, content-independent identity for a
// keyed observation. Two machines that upsert the same (scope, topic_key)
// produce the same sync_id, so import collapses them.
func computeSyncID(scope, topicKey string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + topicKey))
	return "obs-" + hex.EncodeToString(sum[:])[:24]
}

// randomSyncID is the stable identity for a keyless observation. It is assigned
// once at creation and persisted, so re-exporting the same row is idempotent.
func randomSyncID() (string, error) {
	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("memory.randomSyncID: %w", err)
	}
	return "obs-" + id, nil
}

// backfillSyncIDs assigns a sync_id to every observation that lacks one, once.
// Keyed rows get the deterministic id; keyless rows a stable random one.
func (s *Store) backfillSyncIDs() error {
	done, err := s.meta("sync_ids_backfilled")
	if err != nil {
		return fmt.Errorf("memory.backfillSyncIDs: %w", err)
	}
	if done != "" {
		return nil
	}
	rows, err := s.db.Query(`SELECT id, topic_key, scope FROM observations WHERE sync_id = ''`)
	if err != nil {
		return fmt.Errorf("memory.backfillSyncIDs: select: %w", err)
	}
	type pending struct{ id, topicKey, scope string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.topicKey, &p.scope); err != nil {
			rows.Close()
			return fmt.Errorf("memory.backfillSyncIDs: scan: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("memory.backfillSyncIDs: rows: %w", err)
	}
	rows.Close()
	for _, p := range todo {
		sid := computeSyncID(p.scope, p.topicKey)
		if p.topicKey == "" {
			var sidErr error
			sid, sidErr = randomSyncID()
			if sidErr != nil {
				return fmt.Errorf("memory.backfillSyncIDs: %w", sidErr)
			}
		}
		if _, err := s.db.Exec(`UPDATE observations SET sync_id = ? WHERE id = ?`, sid, p.id); err != nil {
			return fmt.Errorf("memory.backfillSyncIDs: update %s: %w", p.id, err)
		}
	}
	if err := s.setMeta("sync_ids_backfilled", "true"); err != nil {
		return fmt.Errorf("memory.backfillSyncIDs: flag: %w", err)
	}
	return nil
}

// backfillTypes populates the type column for legacy rows that carry their type
// only in the topic key prefix (type column empty). Runs once, guarded by a
// metadata flag. Rows whose prefix is not a valid type are left untyped.
func (s *Store) backfillTypes() error {
	done, err := s.meta("types_backfilled")
	if err != nil {
		return fmt.Errorf("memory.backfillTypes: %w", err)
	}
	if done != "" {
		return nil
	}
	rows, err := s.db.Query(`SELECT id, topic_key FROM observations WHERE type = ''`)
	if err != nil {
		return fmt.Errorf("memory.backfillTypes: select: %w", err)
	}
	type pending struct{ id, topicKey string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.topicKey); err != nil {
			rows.Close()
			return fmt.Errorf("memory.backfillTypes: scan: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("memory.backfillTypes: rows: %w", err)
	}
	rows.Close()
	for _, p := range todo {
		t := typeFromTopicKey(p.topicKey)
		if t == "" {
			continue
		}
		if _, err := s.db.Exec(`UPDATE observations SET type = ? WHERE id = ?`, t, p.id); err != nil {
			return fmt.Errorf("memory.backfillTypes: update %s: %w", p.id, err)
		}
	}
	if err := s.setMeta("types_backfilled", "true"); err != nil {
		return fmt.Errorf("memory.backfillTypes: flag: %w", err)
	}
	return nil
}

var validObservationTypes = map[string]bool{
	"architecture": true, "bug-pattern": true, "decision": true,
	"gotcha": true, "reference": true, "skill": true, "testing": true,
}

// typeFromTopicKey returns the topic key prefix (the part before the first "/")
// when it names a valid observation type, else "". The topic key convention is
// "type/slug" (e.g. "bug-pattern/null-slug"); this lets a save without an
// explicit type still populate the type column so --type filtering finds it.
func typeFromTopicKey(topicKey string) string {
	prefix, _, ok := strings.Cut(topicKey, "/")
	if ok && validObservationTypes[prefix] {
		return prefix
	}
	return ""
}

// ObservationsByID resolves observation IDs to observations; non-observation IDs
// (e.g. graph node IDs) are silently skipped.
func (s *Store) ObservationsByID(ids []string) ([]Observation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT %s FROM observations WHERE id IN (%s) AND deleted_at IS NULL
		ORDER BY topic_key, id`, obsColumns, strings.Join(ph, ","))
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.ObservationsByID: %w", err)
	}
	return collectRows(rows)
}

// Rescope changes the scope of the non-deleted observation keyed by
// (topicKey, fromScope, project) to toScope, recomputing its sync_id and bumping
// updated_at. id, revision, content, and relations are preserved (this is an
// in-place UPDATE, not delete+re-add). Returns the updated observation and
// whether a change was made: (Observation{}, false, nil) when no matching row
// exists; (existing, false, nil) when fromScope == toScope; an error when a
// non-deleted row already occupies (topicKey, toScope, project).
func (s *Store) Rescope(topicKey, fromScope, toScope, project string) (Observation, bool, error) {
	if topicKey == "" {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: topic key is required")
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	existing, found, err := findByKeyTx(tx, topicKey, fromScope, project)
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: %w", err)
	}
	if !found {
		return Observation{}, false, nil
	}
	if fromScope == toScope {
		return existing, false, nil
	}
	if _, collide, cerr := findByKeyTx(tx, topicKey, toScope, project); cerr != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: %w", cerr)
	} else if collide {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: scope %q already has topic %q", toScope, topicKey)
	}
	newSync := computeSyncID(toScope, topicKey)
	if _, err := tx.Exec(`UPDATE observations SET scope = ?, sync_id = ?, updated_at = ? WHERE id = ?`,
		toScope, newSync, now.Format(timeFormat), existing.ID); err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Rescope: commit: %w", err)
	}
	existing.Scope = toScope
	existing.SyncID = newSync
	existing.UpdatedAt = now
	return existing, true, nil
}

// Retopic renames the topic_key of the non-deleted observation keyed by
// (oldKey, scope, project) to newKey in place, recomputing its sync_id and
// bumping both revision and updated_at. The revision bump is deliberate: it
// lets the new-sync_id row win the max-revision import rule. id, created_at,
// content, and relations are preserved (relations reference id, which is
// unchanged, so this is an in-place UPDATE, not delete+re-add). Because
// topic_key is an FTS-indexed column, the FTS row is reindexed too. Returns the
// updated observation and whether a change was made: (Observation{}, false, nil)
// when no matching row exists; (existing, false, nil) when oldKey == newKey; an
// error with no mutation when a non-deleted row already occupies
// (newKey, scope, project).
func (s *Store) Retopic(oldKey, newKey, scope, project string) (Observation, bool, error) {
	if oldKey == "" || newKey == "" {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: topic key is required")
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	existing, found, err := findByKeyTx(tx, oldKey, scope, project)
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: %w", err)
	}
	if !found {
		return Observation{}, false, nil
	}
	if oldKey == newKey {
		return existing, false, nil
	}
	if _, collide, cerr := findByKeyTx(tx, newKey, scope, project); cerr != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: %w", cerr)
	} else if collide {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: scope %q already has topic %q", scope, newKey)
	}
	newSync := computeSyncID(scope, newKey)
	if _, err := tx.Exec(`UPDATE observations SET topic_key = ?, sync_id = ?, revision = revision + 1, updated_at = ? WHERE id = ?`,
		newKey, newSync, now.Format(timeFormat), existing.ID); err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: %w", err)
	}
	existing.TopicKey = newKey
	if err := ftsUpdate(tx, s.fts, existing); err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Observation{}, false, fmt.Errorf("memory.Store.Retopic: commit: %w", err)
	}
	existing.SyncID = newSync
	existing.Revision++
	existing.UpdatedAt = now
	return existing, true, nil
}

// Delete soft-deletes the non-deleted observation keyed by (topicKey, scope,
// project): it sets deleted_at and removes the FTS row, so the observation drops
// out of List/Search/Recent/ExportRecords. Idempotent: deleting an absent or
// already-deleted observation returns (false, nil). Deletion is local and does
// not propagate to teammates.
func (s *Store) Delete(topicKey, scope, project string) (bool, error) {
	if topicKey == "" {
		return false, fmt.Errorf("memory.Store.Delete: topic key is required")
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("memory.Store.Delete: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	existing, found, err := findByKeyTx(tx, topicKey, scope, project)
	if err != nil {
		return false, fmt.Errorf("memory.Store.Delete: %w", err)
	}
	if !found {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE observations SET deleted_at = ? WHERE id = ?`,
		now.Format(timeFormat), existing.ID); err != nil {
		return false, fmt.Errorf("memory.Store.Delete: %w", err)
	}
	if err := ftsDelete(tx, s.fts, existing.ID); err != nil {
		return false, fmt.Errorf("memory.Store.Delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("memory.Store.Delete: commit: %w", err)
	}
	return true, nil
}
