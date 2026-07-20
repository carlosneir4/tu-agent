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
// It only creates the index table and migrates schema_version — it never
// rebuilds. The rebuild is lazy: it happens on the search path, and only
// when the "fts_dirty" meta flag says the index may have drifted from
// observations. See maybeRebuildFTS.
func (s *Store) initFTS() error {
	if ftsDisabled || !ftsAvailable(s.db) {
		slog.Warn("memory: SQLite FTS5 module unavailable; search uses substring fallback (build with -tags sqlite_fts5)")
		return nil
	}
	if err := s.setupFTS(); err != nil {
		return err
	}
	s.fts = true
	// Snapshot the count-drift probe once, right here, so maybeRebuildFTS can
	// consult it on the first search without a live re-query. This is a READ
	// only (see countsDrifted) — it writes nothing, so it does not reintroduce
	// the write-on-open contention this feature removed; see maybeRebuildFTS
	// for why the snapshot is taken here instead of recomputed on every
	// search.
	drifted, err := s.countsDrifted()
	if err != nil {
		return fmt.Errorf("memory.Store.initFTS: %w", err)
	}
	s.openDrift = drifted
	return nil
}

// setupFTS creates the index table if absent and migrates schema_version to
// "3", the marker for the dirty-gated rebuild design. Two situations force a
// dirty flag (so the next search performs exactly one curing rebuild)
// instead of trusting the index as-is:
//
//   - schema_version "2": written by a pre-migration binary that always
//     rebuilt on Open, and so never learned to set fts_dirty on its own.
//   - the index table is EMPTY (whether just created by the call above, or
//     already existing) while observations already has live rows: those
//     rows were written before any FTS5-aware code existed for this
//     database (e.g. migrated from JSON, backfilled in place, or inserted
//     directly), so the empty table does not yet reflect them. An existing
//     table is not proof of a synced index: an older setupFTS can commit its
//     CREATE VIRTUAL TABLE and then abort before it counts and flags live
//     rows (e.g. on the "database is locked" error this whole feature
//     exists to fix), leaving exactly this shape behind.
//
// A genuinely fresh database (table just created, observations still
// empty) needs neither: any row added from here on goes through the
// FTS5-enabled branch of ftsInsert, which mirrors it directly.
//
// There are deliberately no SQL triggers: a trigger writing into an FTS5
// table would break every INSERT/UPDATE issued by a binary that lacks the
// module.
func (s *Store) setupFTS() error {
	if _, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts
		USING fts5(id UNINDEXED, title, content, topic_key)`); err != nil {
		return fmt.Errorf("memory.Store.setupFTS: create: %w", err)
	}
	v, err := s.meta("schema_version")
	if err != nil {
		return fmt.Errorf("memory.Store.setupFTS: %w", err)
	}
	if v == "3" {
		return nil
	}
	needsDirty := v == "2"
	if !needsDirty {
		var ftsN int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&ftsN); err != nil {
			return fmt.Errorf("memory.Store.setupFTS: count fts: %w", err)
		}
		if ftsN == 0 {
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL`).Scan(&n); err != nil {
				return fmt.Errorf("memory.Store.setupFTS: count: %w", err)
			}
			needsDirty = n > 0
		}
	}
	if needsDirty {
		if err := s.setMeta("fts_dirty", "true"); err != nil {
			return fmt.Errorf("memory.Store.setupFTS: %w", err)
		}
	}
	if err := s.setMeta("schema_version", "3"); err != nil {
		return fmt.Errorf("memory.Store.setupFTS: %w", err)
	}
	return nil
}

// maybeRebuildFTS rebuilds the index from live observations if a prior
// writer (a binary without the FTS5 module, or the schema_version 2->3
// migration in setupFTS) marked the store dirty, OR if the count-drift probe
// snapshotted at Open (s.openDrift, see initFTS/countsDrifted) found the
// index already out of sync when this Store was opened. A clean, undrifted
// store returns immediately without touching observations_fts — this is
// what keeps a non-searching open (e.g. the SessionStart hooks) from writing
// at all.
//
// The probe exists to catch drift that no writer ever flagged: a binary
// built BEFORE the fts_dirty mechanism existed (its ftsInsert was a bare
// `return nil`, it never called markDirty) can insert or delete observations
// while leaving observations_fts untouched and the flag unset. Comparing
// `count(*) FROM observations WHERE deleted_at IS NULL` against `count(*)
// FROM observations_fts` catches drift that moves the two counts apart. It is
// a heuristic, not a proof of agreement: see the accepted limitations below
// for the two shapes it is blind to.
//
// Why the probe is snapshotted once at Open (in s.openDrift) rather than
// re-queried live on every call here: most callers open a Store immediately
// before doing one piece of work and close it (see withMemStore, which is how
// every MCP tool call and most command bodies reach the store), so a snapshot
// taken at Open is exactly as fresh as a live query would be. The exceptions
// are real and worth naming rather than hand-waving: cmd/tu-agent/tdd.go,
// run.go and chat.go each open one Store for a whole run and hand it to a tool
// registry that includes NewMemSearchTool, so those instances search many times
// over their lifetime. There, a snapshot taken at Open ages: drift an external
// writer introduces mid-run is not seen until the next Open. That is accepted —
// reaching it needs a binary older than the fts_dirty flag AND built without
// the FTS5 module writing concurrently during a run, and the rollout window for
// such a binary closes on its own. (run.go and chat.go are the frozen
// standalone harness per CLAUDE.md §10; tdd.go is not.) Re-querying live on
// every call, instead of
// snapshotting once, cannot tell "an old external writer already left the
// index behind before this Store opened" (must rebuild) apart from "this
// same instance's own index rows were removed out from under it after a
// clean open" (must NOT rebuild — @s2/@s4 in fts_internal_test.go pin this:
// they tamper by deleting straight from observations_fts on an already-open,
// already-clean Store and require the next search to leave that gap alone).
// Both produce the identical count mismatch, so only the moment of
// evaluation — at Open, before this instance has done anything — can tell
// them apart. Snapshotting is a plain read (see countsDrifted) and writes
// nothing, so it does not reintroduce the write-on-open contention this
// feature removed.
//
// Known and accepted limitations. The probe is blind to three shapes, and
// none of them is closed here — do not read the probe as a guarantee:
//
//  1. Compensating drift. A pre-fts_dirty binary that adds one observation
//     and deletes another moves the live count +1-1 = 0 while the index count
//     does not move at all, so both counts agree and the probe sees nothing.
//     The added note is then invisible to ranked search, permanently.
//  2. Edits. That same binary UPDATEing an observation's text leaves both
//     counts equal while the indexed text goes stale.
//  3. Aging. Only drift already present at Open is seen (see above).
//
// All three need a binary older than fts_dirty AND built without the FTS5
// module: every binary built since marks dirty on insert, delete and update
// (see ftsInsert/ftsDelete/ftsUpdate -> markDirty), and every released binary
// carries the module (release.yml builds with the tag), so reaching any of
// them takes a local tag-less build from before this change. That rollout
// window closes on its own, which is why the gaps are accepted rather than
// chased with a scheme that would have to hash content to be sound.
//
// rebuildFTS clears the "fts_dirty" flag itself, atomically with its rebuild
// (see rebuildFTS) — this function also clears s.openDrift once it rebuilds,
// so a later search on the same instance does not rebuild again for a
// snapshot that has already been cured.
func (s *Store) maybeRebuildFTS() error {
	dirty, err := s.meta("fts_dirty")
	if err != nil {
		return fmt.Errorf("memory.Store.maybeRebuildFTS: %w", err)
	}
	if dirty == "" && !s.openDrift {
		return nil
	}
	if err := s.rebuildFTS(); err != nil {
		return fmt.Errorf("memory.Store.maybeRebuildFTS: rebuild: %w", err)
	}
	s.openDrift = false
	return nil
}

// countsDrifted is the cheap drift probe consulted by initFTS/maybeRebuildFTS
// (see maybeRebuildFTS for why it is snapshotted at Open rather than
// re-queried live on every search). Both queries are reads.
func (s *Store) countsDrifted() (bool, error) {
	var obsN, ftsN int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL`).Scan(&obsN); err != nil {
		return false, fmt.Errorf("memory.Store.countsDrifted: count observations: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&ftsN); err != nil {
		return false, fmt.Errorf("memory.Store.countsDrifted: count observations_fts: %w", err)
	}
	return obsN != ftsN, nil
}

// rebuildFTS refills the index from live observations and clears the
// "fts_dirty" flag in the SAME transaction as the DELETE+INSERT, via
// clearDirty. The transaction begins as BEGIN IMMEDIATE (the store's DSN
// sets _txlock=immediate — see Open), so it holds the write lock for its
// entire BEGIN-to-COMMIT span: a concurrent writer without the FTS5 module
// cannot commit its own observation-plus-markDirty inside that span, so
// there is no window left in which this rebuild's clear could race ahead of
// — and silently erase — a dirty mark that writer just set. Clearing the
// flag as a later, separate, autocommit write (the old design) left exactly
// that window open; folding it into this commit closes it entirely.
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
	if err := clearDirty(tx); err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory.Store.rebuildFTS: commit: %w", err)
	}
	return nil
}

// markDirty flags the store as needing a rebuild the next time something
// searches. Used by ftsInsert/ftsDelete when a writer has no FTS5 module and
// so cannot mirror its change into observations_fts directly. Takes an
// execer (rather than calling Store.setMeta) so callers can write it inside
// their own open transaction — calling setMeta there would try to open a
// second transaction on the same connection and deadlock.
func markDirty(db execer) error {
	if _, err := db.Exec(`INSERT INTO metadata(key, value) VALUES('fts_dirty', 'true')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return fmt.Errorf("memory.markDirty: %w", err)
	}
	return nil
}

// clearDirty resets the "fts_dirty" flag. Used by rebuildFTS to clear it
// atomically with its own DELETE+INSERT commit (see rebuildFTS). Takes an
// execer for the same reason as markDirty: called inside rebuildFTS's open
// transaction, so it must not open a second transaction of its own.
func clearDirty(db execer) error {
	if _, err := db.Exec(`INSERT INTO metadata(key, value) VALUES('fts_dirty', '')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return fmt.Errorf("memory.clearDirty: %w", err)
	}
	return nil
}

// ftsInsert mirrors a new observation into the index. When fts is off (no
// FTS5 module), it instead marks the store dirty so a later FTS5-enabled
// search cures the gap.
func ftsInsert(db execer, fts bool, o Observation) error {
	if !fts {
		return markDirty(db)
	}
	if _, err := db.Exec(`INSERT INTO observations_fts(id, title, content, topic_key)
		VALUES (?,?,?,?)`, o.ID, o.Title, o.Content, o.TopicKey); err != nil {
		return fmt.Errorf("memory.ftsInsert: %w", err)
	}
	return nil
}

// ftsDelete removes an observation from the FTS index by id. When fts is
// off, it instead marks the store dirty (see ftsInsert).
func ftsDelete(db execer, fts bool, id string) error {
	if !fts {
		return markDirty(db)
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
// breaks ties newest-first. limit caps the returned rows (0 = uncapped); the
// second return is the total match count before the cap so a caller can show
// an honest "showing N of M" disclosure.
func (s *Store) searchFTS(query, typeFilter string, limit int) ([]Observation, int, error) {
	if err := s.maybeRebuildFTS(); err != nil {
		return nil, 0, fmt.Errorf("memory.Store.searchFTS: %w", err)
	}
	countSQL := `SELECT count(*)
		FROM observations_fts
		JOIN observations o ON o.id = observations_fts.id
		WHERE observations_fts MATCH ? AND o.deleted_at IS NULL`
	sql := `SELECT o.id, o.topic_key, o.scope, o.project, o.title, o.content,
			o.type, o.source, o.revision, o.created_at, o.updated_at, o.author, o.sync_id, o.imported
		FROM observations_fts
		JOIN observations o ON o.id = observations_fts.id
		WHERE observations_fts MATCH ? AND o.deleted_at IS NULL`
	args := []any{ftsQuery(query)}
	if typeFilter != "" {
		countSQL += ` AND o.type = ?`
		sql += ` AND o.type = ?`
		args = append(args, typeFilter)
	}
	var total int
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("memory.Store.searchFTS: count: %w", err)
	}
	sql += ` ORDER BY bm25(observations_fts), o.updated_at DESC`
	if limit > 0 {
		sql += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("memory.Store.searchFTS: %w", err)
	}
	out, err := collectRows(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("memory.Store.searchFTS: %w", err)
	}
	return out, total, nil
}
