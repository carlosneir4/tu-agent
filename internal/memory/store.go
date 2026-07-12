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
}

// Open opens (or creates) the memory database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("memory.Open: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000")
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
	obs := Observation{
		ID: newID(), Scope: "project", Title: topic, Content: content,
		Source: source, SyncID: randomSyncID(), Revision: 1, CreatedAt: now, UpdatedAt: now,
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
		obs := Observation{
			ID: newID(), TopicKey: topicKey, Scope: scope, Title: title,
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

// newID returns a random 16-hex-char identifier.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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
func randomSyncID() string { return "obs-" + newID() }

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
		sid := randomSyncID()
		if p.topicKey != "" {
			sid = computeSyncID(p.scope, p.topicKey)
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

// Relation is a typed link between two entities (observation IDs or graph node IDs).
type Relation struct {
	ID        string
	FromID    string
	ToID      string
	Type      string
	CreatedAt time.Time
}

var validRelationTypes = map[string]bool{
	"related": true, "supersedes": true, "documents": true,
	"documents_auto": true, "conflicts_with": true,
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

// Relate upserts a typed relation. Re-relating the same (from,to,type) is a no-op.
func (s *Store) Relate(fromID, toID, relType string) (Relation, error) {
	if relType == "" {
		relType = "related"
	}
	if !validRelationTypes[relType] {
		return Relation{}, fmt.Errorf("memory.Store.Relate: invalid relation type %q", relType)
	}
	if fromID == "" || toID == "" {
		return Relation{}, fmt.Errorf("memory.Store.Relate: from and to are required")
	}
	now := time.Now().UTC()
	rel := Relation{ID: newID(), FromID: fromID, ToID: toID, Type: relType, CreatedAt: now}
	if _, err := s.db.Exec(`INSERT INTO memory_relations(id, from_id, to_id, relation_type, created_at)
		VALUES(?,?,?,?,?) ON CONFLICT(from_id, to_id, relation_type) DO NOTHING`,
		rel.ID, fromID, toID, relType, now.Format(timeFormat)); err != nil {
		return Relation{}, fmt.Errorf("memory.Store.Relate: %w", err)
	}
	return rel, nil
}

// RelationsTo returns relations whose to_id is in ids.
func (s *Store) RelationsTo(ids []string) ([]Relation, error) { return s.relationsBy("to_id", ids) }

// RelationsFrom returns relations whose from_id is in ids.
func (s *Store) RelationsFrom(ids []string) ([]Relation, error) { return s.relationsBy("from_id", ids) }

// relationsBy queries by a fixed column ("to_id" or "from_id"); col is never user input.
func (s *Store) relationsBy(col string, ids []string) ([]Relation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT id, from_id, to_id, relation_type, created_at FROM memory_relations
		WHERE %s IN (%s) ORDER BY created_at, id`, col, strings.Join(ph, ","))
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.relationsBy: %w", err)
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		var created string
		if err := rows.Scan(&r.ID, &r.FromID, &r.ToID, &r.Type, &created); err != nil {
			return nil, fmt.Errorf("memory.Store.relationsBy: scan: %w", err)
		}
		r.CreatedAt, _ = time.Parse(timeParseFormat, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RelationsByType returns all relations with the given relation_type, ordered
// stably. Used to list, e.g., every conflicts_with edge.
func (s *Store) RelationsByType(relType string) ([]Relation, error) {
	rows, err := s.db.Query(`SELECT id, from_id, to_id, relation_type, created_at FROM memory_relations
		WHERE relation_type = ? ORDER BY created_at, id`, relType)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.RelationsByType: %w", err)
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		var created string
		if err := rows.Scan(&r.ID, &r.FromID, &r.ToID, &r.Type, &created); err != nil {
			return nil, fmt.Errorf("memory.Store.RelationsByType: scan: %w", err)
		}
		r.CreatedAt, _ = time.Parse(timeParseFormat, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRelationsByType removes all relations with the given from_id and
// relation_type, returning the number deleted. Used to re-derive auto-links
// without disturbing hand-curated relations of other types.
func (s *Store) DeleteRelationsByType(fromID, relType string) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM memory_relations WHERE from_id = ? AND relation_type = ?`,
		fromID, relType)
	if err != nil {
		return 0, fmt.Errorf("memory.Store.DeleteRelationsByType: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("memory.Store.DeleteRelationsByType: %w", err)
	}
	return int(n), nil
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

// Session is one explicit work session.
type Session struct {
	ID        string
	Project   string
	StartedAt time.Time
	EndedAt   time.Time // zero value while active
	Summary   string
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

// SessionStart opens a new session for project. Any still-open session for the
// project is auto-ended first (composed summary) so at most one is ever active.
// Returns the new session and the previous (now most-recent ended) summary.
func (s *Store) SessionStart(project string) (Session, string, error) {
	if active, found, err := s.activeSession(project); err != nil {
		return Session{}, "", err
	} else if found {
		if _, err := s.endSession(active.ID, project, ""); err != nil {
			return Session{}, "", err
		}
	}
	prev, err := s.LastSummary(project)
	if err != nil {
		return Session{}, "", err
	}
	now := time.Now().UTC()
	sess := Session{ID: newID(), Project: project, StartedAt: now}
	if _, err := s.db.Exec(
		`INSERT INTO sessions(id, project, started_at, ended_at, summary) VALUES(?,?,?,NULL,'')`,
		sess.ID, sess.Project, now.Format(timeFormat)); err != nil {
		return Session{}, "", fmt.Errorf("memory.Store.SessionStart: %w", err)
	}
	return sess, prev, nil
}

// SessionEnd closes the active session for project, composing a summary from the
// session's observations when summary is empty. Errors if no session is active.
func (s *Store) SessionEnd(project, summary string) (Session, error) {
	active, found, err := s.activeSession(project)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("memory.Store.SessionEnd: no active session for %q", project)
	}
	return s.endSession(active.ID, project, summary)
}

func (s *Store) endSession(id, project, summary string) (Session, error) {
	var startedStr string
	if err := s.db.QueryRow(`SELECT started_at FROM sessions WHERE id = ?`, id).Scan(&startedStr); err != nil {
		return Session{}, fmt.Errorf("memory.Store.endSession: load: %w", err)
	}
	started, _ := time.Parse(timeParseFormat, startedStr)
	now := time.Now().UTC()
	if summary == "" {
		summary = s.composeSummary(started, now)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET ended_at = ?, summary = ? WHERE id = ?`,
		now.Format(timeFormat), summary, id); err != nil {
		return Session{}, fmt.Errorf("memory.Store.endSession: update: %w", err)
	}
	return Session{ID: id, Project: project, StartedAt: started, EndedAt: now, Summary: summary}, nil
}

// composeSummary builds a deterministic summary from observations created in
// [start, end]. The memory DB is repo-local, so no project filter is needed.
func (s *Store) composeSummary(start, end time.Time) string {
	rows, err := s.db.Query(`SELECT topic_key, title FROM observations
		WHERE deleted_at IS NULL AND created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC LIMIT 10`,
		start.Format(timeFormat), end.Format(timeFormat))
	if err != nil {
		return "(no observations recorded)"
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var tk, title string
		if err := rows.Scan(&tk, &title); err != nil {
			continue
		}
		label := tk
		if label == "" {
			label = title
		}
		if label != "" {
			items = append(items, label)
		}
	}
	if len(items) == 0 {
		return "(no observations recorded)"
	}
	return fmt.Sprintf("%d observation(s): %s", len(items), strings.Join(items, "; "))
}

func (s *Store) activeSession(project string) (Session, bool, error) {
	var sess Session
	var started string
	err := s.db.QueryRow(`SELECT id, project, started_at FROM sessions
		WHERE project = ? AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, project).
		Scan(&sess.ID, &sess.Project, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("memory.Store.activeSession: %w", err)
	}
	sess.StartedAt, _ = time.Parse(timeParseFormat, started)
	return sess, true, nil
}

// LastSummary returns the most recent ended session's summary for project ("" if none).
func (s *Store) LastSummary(project string) (string, error) {
	var summary string
	err := s.db.QueryRow(`SELECT summary FROM sessions
		WHERE project = ? AND ended_at IS NOT NULL ORDER BY ended_at DESC LIMIT 1`, project).Scan(&summary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("memory.Store.LastSummary: %w", err)
	}
	return summary, nil
}

// SessionList returns the most recent n sessions for project, newest first.
func (s *Store) SessionList(project string, n int) ([]Session, error) {
	if n <= 0 {
		n = 10
	}
	rows, err := s.db.Query(`SELECT id, project, started_at, ended_at, summary FROM sessions
		WHERE project = ? ORDER BY started_at DESC, id DESC LIMIT ?`, project, n)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.SessionList: %w", err)
	}
	defer rows.Close()
	out := make([]Session, 0, n)
	for rows.Next() {
		var sess Session
		var started string
		var ended sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Project, &started, &ended, &sess.Summary); err != nil {
			return nil, fmt.Errorf("memory.Store.SessionList: scan: %w", err)
		}
		sess.StartedAt, _ = time.Parse(timeParseFormat, started)
		if ended.Valid && ended.String != "" {
			sess.EndedAt, _ = time.Parse(timeParseFormat, ended.String)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}
