package memory

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// ftsDisabled forces the LIKE fallback. Tests set it to cover binaries
// built without the sqlite_fts5 tag.
var ftsDisabled bool

// ftsAvailable probes the connection for the FTS5 module. Binaries built
// without -tags sqlite_fts5 do not include it.
func ftsAvailable(db *sql.DB) bool {
	if _, err := db.Exec(`CREATE VIRTUAL TABLE temp.fts_probe USING fts5(x)`); err != nil {
		return false
	}
	_, _ = db.Exec(`DROP TABLE temp.fts_probe`)
	return true
}

// initFTS enables full-text search when the FTS5 module is compiled in.
// Called at the end of Open so the rebuild also indexes JSON-migrated rows.
func (s *Store) initFTS() error {
	if ftsDisabled || !ftsAvailable(s.db) {
		slog.Warn("memory: SQLite FTS5 module unavailable; search uses substring fallback (build with -tags sqlite_fts5)")
		return nil
	}
	if err := s.setupFTS(); err != nil {
		return err
	}
	s.fts = true
	return nil
}

// setupFTS creates the index table and rebuilds it from observations.
// There are deliberately no SQL triggers: a trigger writing into an FTS5
// table would break every INSERT/UPDATE issued by a binary that lacks the
// module. Rebuilding on each open heals the drift those writers leave.
func (s *Store) setupFTS() error {
	if _, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts
		USING fts5(id UNINDEXED, title, content, topic_key)`); err != nil {
		return fmt.Errorf("memory.Store.setupFTS: create: %w", err)
	}
	if err := s.rebuildFTS(); err != nil {
		return fmt.Errorf("memory.Store.setupFTS: rebuild: %w", err)
	}
	if err := s.setMeta("schema_version", "2"); err != nil {
		return err
	}
	return nil
}

// rebuildFTS refills the index from live observations.
func (s *Store) rebuildFTS() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM observations_fts`); err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: clear: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO observations_fts(id, title, content, topic_key)
		SELECT id, title, content, topic_key FROM observations WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: backfill: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: commit: %w", err)
	}
	return nil
}

// ftsInsert mirrors a new observation into the index. No-op when fts is off.
func ftsInsert(db execer, fts bool, o Observation) error {
	if !fts {
		return nil
	}
	if _, err := db.Exec(`INSERT INTO observations_fts(id, title, content, topic_key)
		VALUES (?,?,?,?)`, o.ID, o.Title, o.Content, o.TopicKey); err != nil {
		return fmt.Errorf("memory.ftsInsert: %w", err)
	}
	return nil
}

// ftsDelete removes an observation from the FTS index by id. No-op when FTS is off.
func ftsDelete(db execer, fts bool, id string) error {
	if !fts {
		return nil
	}
	if _, err := db.Exec(`DELETE FROM observations_fts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("memory.ftsDelete: %w", err)
	}
	return nil
}

// ftsUpdate re-indexes an observation after its text columns changed.
func ftsUpdate(db execer, fts bool, o Observation) error {
	if err := ftsDelete(db, fts, o.ID); err != nil {
		return err
	}
	return ftsInsert(db, fts, o)
}

// ftsQuery converts free text into an FTS5 MATCH expression: each whitespace-
// separated token is double-quoted (neutralising operator syntax in user input)
// and OR-joined so a query spanning several observations returns all of them.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// searchFTS runs a ranked MATCH query. bm25() returns smaller (more negative)
// values for better matches, so ASC puts the best match first; updated_at
// breaks ties newest-first.
func (s *Store) searchFTS(query, typeFilter string) ([]Observation, error) {
	sql := `SELECT o.id, o.topic_key, o.scope, o.project, o.title, o.content,
			o.type, o.source, o.revision, o.created_at, o.updated_at, o.author, o.sync_id
		FROM observations_fts
		JOIN observations o ON o.id = observations_fts.id
		WHERE observations_fts MATCH ? AND o.deleted_at IS NULL`
	args := []any{ftsQuery(query)}
	if typeFilter != "" {
		sql += ` AND o.type = ?`
		args = append(args, typeFilter)
	}
	sql += ` ORDER BY bm25(observations_fts), o.updated_at DESC`
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.searchFTS: %w", err)
	}
	out, err := collectRows(rows)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.searchFTS: %w", err)
	}
	return out, nil
}
