// Package store persists the knowledge graph in SQLite. The graph is derived
// data: any open that finds a schema or extractor version mismatch deletes
// the database and starts fresh.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/tu/tu-agent/internal/graph"
)

const schemaVersion = "6"

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL,
  path TEXT NOT NULL, line INT, end_line INT, language TEXT,
  params TEXT NOT NULL DEFAULT '', return_type TEXT NOT NULL DEFAULT '',
  exported INT NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_nodes_path ON nodes(path);
CREATE TABLE IF NOT EXISTS edges (
  from_id TEXT NOT NULL, to_id TEXT NOT NULL, kind TEXT NOT NULL,
  confidence TEXT NOT NULL DEFAULT 'exact',
  PRIMARY KEY (from_id, to_id, kind));
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
CREATE TABLE IF NOT EXISTS refs (
  from_id TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL, line INT,
  recv TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_refs_from ON refs(from_id);
CREATE TABLE IF NOT EXISTS files (
  path TEXT PRIMARY KEY, sha256 TEXT NOT NULL, language TEXT,
  status TEXT NOT NULL DEFAULT 'ok', package TEXT DEFAULT '',
  imports TEXT DEFAULT '[]', size INT DEFAULT 0, mtime_ns INT DEFAULT 0,
  parsed_at TEXT);
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS concepts (
  name        TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL DEFAULT '');
`

// FileRecord mirrors one row of the files table.
type FileRecord struct {
	Path, SHA256, Language, Status, Package string
	Imports                                 []string
	Size                                    int
	MtimeNS                                 int64
}

// Store wraps the SQLite connection.
type Store struct{ db *sql.DB }

// Open opens (or creates) the database at path. A schema or extractor
// version mismatch deletes the file and recreates it — the graph is
// derived data, so this is always safe.
func Open(path, extractorVersion string) (*Store, error) {
	for attempt := 0; ; attempt++ {
		db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
		if err != nil {
			return nil, fmt.Errorf("graph.Store.Open: %w", err)
		}
		if _, err := db.Exec(schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("graph.Store.Open: applying schema: %w", err)
		}
		// Serialize all writes through a single connection; SQLite does not support
		// concurrent writers and WAL mode does not change that constraint.
		db.SetMaxOpenConns(1)
		s := &Store{db: db}
		sv, _ := s.Meta("schema_version")
		ev, _ := s.Meta("extractor_version")
		fresh := sv == "" && ev == ""
		if fresh {
			if err := s.setMeta("schema_version", schemaVersion); err != nil {
				s.Close()
				return nil, err
			}
			if err := s.setMeta("extractor_version", extractorVersion); err != nil {
				s.Close()
				return nil, err
			}
			return s, nil
		}
		if sv == schemaVersion && ev == extractorVersion {
			return s, nil
		}
		// Version mismatch: rebuild once, never loop.
		s.Close()
		if attempt > 0 {
			return nil, fmt.Errorf("graph.Store.Open: version mismatch persists after rebuild")
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("graph.Store.Open: removing stale db: %w", err)
		}
		if err := os.Remove(path + "-wal"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("graph.Store.Open: removing stale wal sidecar: %w", err)
		}
		if err := os.Remove(path + "-shm"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("graph.Store.Open: removing stale shm sidecar: %w", err)
		}
	}
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Meta returns the metadata value for key ("" if absent).
func (s *Store) Meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("graph.Store.Meta: %w", err)
	}
	return v, nil
}

func (s *Store) setMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO metadata(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("graph.Store.setMeta: %w", err)
	}
	return nil
}

// execer is the subset of *sql.DB / *sql.Tx used by the row-writing helpers, so
// the same SQL runs either standalone (autocommit) or inside a caller's tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// UpsertFile inserts or replaces one files row.
func (s *Store) UpsertFile(f FileRecord) error {
	if err := upsertFileRow(s.db, f); err != nil {
		return fmt.Errorf("graph.Store.UpsertFile: %w", err)
	}
	return nil
}

// upsertFileRow writes one files row via x (a *sql.DB for autocommit, or an open
// *sql.Tx to fold the write into a caller's transaction).
func upsertFileRow(x execer, f FileRecord) error {
	imp, err := json.Marshal(f.Imports)
	if err != nil {
		return err
	}
	_, err = x.Exec(`INSERT INTO files(path,sha256,language,status,package,imports,size,mtime_ns,parsed_at)
		VALUES(?,?,?,?,?,?,?,?,datetime('now'))
		ON CONFLICT(path) DO UPDATE SET sha256=excluded.sha256, language=excluded.language,
		status=excluded.status, package=excluded.package, imports=excluded.imports,
		size=excluded.size, mtime_ns=excluded.mtime_ns, parsed_at=excluded.parsed_at`,
		f.Path, f.SHA256, f.Language, f.Status, f.Package, string(imp), f.Size, f.MtimeNS)
	return err
}

// Files returns every files row keyed by path.
func (s *Store) Files() (map[string]FileRecord, error) {
	rows, err := s.db.Query(`SELECT path,sha256,language,status,package,imports,size,mtime_ns FROM files`)
	if err != nil {
		return nil, fmt.Errorf("graph.Store.Files: %w", err)
	}
	defer rows.Close()
	out := map[string]FileRecord{}
	for rows.Next() {
		var f FileRecord
		var imp string
		if err := rows.Scan(&f.Path, &f.SHA256, &f.Language, &f.Status, &f.Package, &imp, &f.Size, &f.MtimeNS); err != nil {
			return nil, fmt.Errorf("graph.Store.Files: %w", err)
		}
		if err := json.Unmarshal([]byte(imp), &f.Imports); err != nil {
			return nil, fmt.Errorf("graph.Store.Files: imports of %s: %w", f.Path, err)
		}
		out[f.Path] = f
	}
	return out, rows.Err()
}

// ReplaceFileNodes atomically replaces all nodes, refs, and contains edges
// belonging to one file. Called once per parsed file.
func (s *Store) ReplaceFileNodes(path string, nodes []graph.Node, refs []graph.Ref, contains []graph.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileNodes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := replaceFileNodesTx(tx, path, nodes, refs, contains); err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileNodes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileNodes: commit: %w", err)
	}
	return nil
}

// ReplaceFileAndNodes upserts a file's state row together with its nodes, refs,
// and contains edges in ONE transaction. The reconcile in graph.Build keys
// change-detection off the files row (matching stored mtime/size/sha against
// disk), so the files row must never be persisted ahead of the nodes it
// summarises. Were the two written in separate transactions, an interrupted
// build (Ctrl-C, CI timeout, OOM kill) could commit the files row with no
// nodes, and every later build would then skip that file as "unchanged",
// orphaning it permanently. Written together, an interrupted build leaves
// neither or both.
func (s *Store) ReplaceFileAndNodes(f FileRecord, nodes []graph.Node, refs []graph.Ref, contains []graph.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileAndNodes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertFileRow(tx, f); err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileAndNodes: file row: %w", err)
	}
	if err := replaceFileNodesTx(tx, f.Path, nodes, refs, contains); err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileAndNodes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.ReplaceFileAndNodes: commit: %w", err)
	}
	return nil
}

// replaceFileNodesTx clears and rewrites one file's nodes, refs, and contains
// edges via tx. Shared by ReplaceFileNodes and ReplaceFileAndNodes.
func replaceFileNodesTx(tx execer, path string, nodes []graph.Node, refs []graph.Ref, contains []graph.Edge) error {
	if _, err := tx.Exec(`DELETE FROM refs WHERE from_id IN (SELECT id FROM nodes WHERE path = ?)`, path); err != nil {
		return fmt.Errorf("clearing refs: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM edges WHERE kind = 'contains' AND from_id IN (SELECT id FROM nodes WHERE path = ?)`, path); err != nil {
		return fmt.Errorf("clearing contains: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE path = ?`, path); err != nil {
		return fmt.Errorf("clearing nodes: %w", err)
	}
	for _, n := range nodes {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO nodes(id,kind,name,path,line,end_line,language,params,return_type,exported) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			n.ID, string(n.Kind), n.Name, n.Path, n.Line, n.EndLine, n.Language, n.Params, n.ReturnType, n.Exported); err != nil {
			return fmt.Errorf("inserting node %s: %w", n.ID, err)
		}
	}
	for _, r := range refs {
		if _, err := tx.Exec(`INSERT INTO refs(from_id,kind,name,line,recv) VALUES(?,?,?,?,?)`,
			r.FromID, string(r.Kind), r.Name, r.Line, r.Recv); err != nil {
			return fmt.Errorf("inserting ref: %w", err)
		}
	}
	for _, e := range contains {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO edges(from_id,to_id,kind,confidence) VALUES(?,?,?,?)`,
			e.From, e.To, string(e.Kind), string(e.Confidence)); err != nil {
			return fmt.Errorf("inserting edge: %w", err)
		}
	}
	return nil
}

// DeleteFile removes a file's row plus all its nodes, refs, and edges
// (both directions).
func (s *Store) DeleteFile(path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.DeleteFile: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := []string{
		`DELETE FROM refs WHERE from_id IN (SELECT id FROM nodes WHERE path = ?)`,
		`DELETE FROM edges WHERE from_id IN (SELECT id FROM nodes WHERE path = ?)
		   OR to_id IN (SELECT id FROM nodes WHERE path = ?)`,
		`DELETE FROM nodes WHERE path = ?`,
		`DELETE FROM files WHERE path = ?`,
	}
	args := [][]any{{path}, {path, path}, {path}, {path}}
	for i, q := range stmts {
		if _, err := tx.Exec(q, args[i]...); err != nil {
			return fmt.Errorf("graph.Store.DeleteFile: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.DeleteFile: commit: %w", err)
	}
	return nil
}

// ReplaceResolvedEdges replaces every non-contains edge in one transaction.
// Resolution is global, so resolved edges are always rewritten wholesale.
// extNodes are external stub nodes synthesised during resolution; they are
// cleared and rewritten in the same transaction so edges are never orphaned.
func (s *Store) ReplaceResolvedEdges(edges []graph.Edge, extNodes []graph.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.ReplaceResolvedEdges: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM edges WHERE kind NOT IN ('contains','documents')`); err != nil {
		return fmt.Errorf("graph.Store.ReplaceResolvedEdges: clearing edges: %w", err)
	}
	// External stub nodes are owned by resolution: clear and rewrite wholesale.
	if _, err := tx.Exec(`DELETE FROM nodes WHERE kind = 'external'`); err != nil {
		return fmt.Errorf("graph.Store.ReplaceResolvedEdges: clearing external nodes: %w", err)
	}
	for _, n := range extNodes {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO nodes(id,kind,name,path,line,end_line,language,params,return_type,exported) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			n.ID, string(n.Kind), n.Name, n.Path, n.Line, n.EndLine, n.Language, n.Params, n.ReturnType, n.Exported); err != nil {
			return fmt.Errorf("graph.Store.ReplaceResolvedEdges: inserting external node %s: %w", n.ID, err)
		}
	}
	for _, e := range edges {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO edges(from_id,to_id,kind,confidence) VALUES(?,?,?,?)`,
			e.From, e.To, string(e.Kind), string(e.Confidence)); err != nil {
			return fmt.Errorf("graph.Store.ReplaceResolvedEdges: inserting edge: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.ReplaceResolvedEdges: commit: %w", err)
	}
	return nil
}

// ReplaceKnowledge replaces all skill/convention nodes and documents edges in
// one transaction. The knowledge layer is rewritten wholesale by learn's
// register step; it is independent of code re-resolution.
func (s *Store) ReplaceKnowledge(nodes []graph.Node, edges []graph.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.ReplaceKnowledge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM edges WHERE kind = 'documents'`); err != nil {
		return fmt.Errorf("graph.Store.ReplaceKnowledge: clearing edges: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE kind IN ('skill','convention')`); err != nil {
		return fmt.Errorf("graph.Store.ReplaceKnowledge: clearing nodes: %w", err)
	}
	for _, n := range nodes {
		if _, err := tx.Exec(`INSERT INTO nodes(id,kind,name,path,line,end_line,language,params,return_type,exported) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			n.ID, string(n.Kind), n.Name, n.Path, n.Line, n.EndLine, n.Language, n.Params, n.ReturnType, n.Exported); err != nil {
			return fmt.Errorf("graph.Store.ReplaceKnowledge: inserting node %s: %w", n.ID, err)
		}
	}
	for _, e := range edges {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO edges(from_id,to_id,kind,confidence) VALUES(?,?,?,?)`,
			e.From, e.To, string(e.Kind), string(e.Confidence)); err != nil {
			return fmt.Errorf("graph.Store.ReplaceKnowledge: inserting edge: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.ReplaceKnowledge: commit: %w", err)
	}
	return nil
}

// ConceptRow is one concept card persisted in the graph store: the rendered
// SKILL.md content plus its frontmatter description, keyed by concept name.
type ConceptRow struct {
	Name        string
	Description string
	Content     string
}

// ReplaceConcepts wholesale-replaces the concepts table in one transaction.
func (s *Store) ReplaceConcepts(rows []ConceptRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("graph.Store.ReplaceConcepts: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM concepts`); err != nil {
		return fmt.Errorf("graph.Store.ReplaceConcepts: clearing: %w", err)
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO concepts(name,description,content) VALUES(?,?,?)`,
			r.Name, r.Description, r.Content); err != nil {
			return fmt.Errorf("graph.Store.ReplaceConcepts: inserting %s: %w", r.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("graph.Store.ReplaceConcepts: commit: %w", err)
	}
	return nil
}

// UpsertConcept inserts or replaces a single concept row by name. Used to write
// back a generated definition without disturbing the rest of the table.
func (s *Store) UpsertConcept(r ConceptRow) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO concepts(name,description,content) VALUES(?,?,?)`,
		r.Name, r.Description, r.Content); err != nil {
		return fmt.Errorf("graph.Store.UpsertConcept: %w", err)
	}
	return nil
}

// GetConcept returns the row for name and whether it exists.
func (s *Store) GetConcept(name string) (ConceptRow, bool, error) {
	var r ConceptRow
	err := s.db.QueryRow(`SELECT name,description,content FROM concepts WHERE name = ?`, name).
		Scan(&r.Name, &r.Description, &r.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return ConceptRow{}, false, nil
	}
	if err != nil {
		return ConceptRow{}, false, fmt.Errorf("graph.Store.GetConcept: %w", err)
	}
	return r, true, nil
}

// ListConcepts returns every concept row, ordered by name.
func (s *Store) ListConcepts() ([]ConceptRow, error) {
	rows, err := s.db.Query(`SELECT name,description,content FROM concepts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("graph.Store.ListConcepts: %w", err)
	}
	defer rows.Close()
	var out []ConceptRow
	for rows.Next() {
		var r ConceptRow
		if err := rows.Scan(&r.Name, &r.Description, &r.Content); err != nil {
			return nil, fmt.Errorf("graph.Store.ListConcepts: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllNodes returns every node, ordered by id.
func (s *Store) AllNodes() ([]graph.Node, error) {
	rows, err := s.db.Query(`SELECT id,kind,name,path,line,end_line,language,params,return_type,exported FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("graph.Store.AllNodes: %w", err)
	}
	defer rows.Close()
	var out []graph.Node
	for rows.Next() {
		var n graph.Node
		var kind string
		if err := rows.Scan(&n.ID, &kind, &n.Name, &n.Path, &n.Line, &n.EndLine, &n.Language, &n.Params, &n.ReturnType, &n.Exported); err != nil {
			return nil, fmt.Errorf("graph.Store.AllNodes: %w", err)
		}
		n.Kind = graph.NodeKind(kind)
		out = append(out, n)
	}
	return out, rows.Err()
}

// NodeCount returns the total number of node rows. Used as a cheap guard:
// callers skip graph cross-checks when the graph is empty/unbuilt.
func (s *Store) NodeCount() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM nodes`).Scan(&n); err != nil {
		return 0, fmt.Errorf("graph.Store.NodeCount: %w", err)
	}
	return n, nil
}

// FileCount returns the total number of files rows. Paired with NodeCount to
// detect the silent-empty-graph state (files present, zero nodes).
func (s *Store) FileCount() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&n); err != nil {
		return 0, fmt.Errorf("graph.Store.FileCount: %w", err)
	}
	return n, nil
}

// ExistingNodeIDs returns the subset of ids that exist as node rows. Batched to
// stay under SQLite's parameter limit. Empty ids → empty map.
func (s *Store) ExistingNodeIDs(ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	const chunk = 500
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		ph := make([]string, len(batch))
		args := make([]any, len(batch))
		for j, id := range batch {
			ph[j] = "?"
			args[j] = id
		}
		rows, err := s.db.Query(`SELECT id FROM nodes WHERE id IN (`+strings.Join(ph, ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("graph.Store.ExistingNodeIDs: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("graph.Store.ExistingNodeIDs: %w", err)
			}
			out[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("graph.Store.ExistingNodeIDs: %w", err)
		}
	}
	return out, nil
}

// AllRefs returns every unresolved ref.
func (s *Store) AllRefs() ([]graph.Ref, error) {
	rows, err := s.db.Query(`SELECT from_id,kind,name,line,recv FROM refs`)
	if err != nil {
		return nil, fmt.Errorf("graph.Store.AllRefs: %w", err)
	}
	defer rows.Close()
	var out []graph.Ref
	for rows.Next() {
		var r graph.Ref
		var kind string
		if err := rows.Scan(&r.FromID, &kind, &r.Name, &r.Line, &r.Recv); err != nil {
			return nil, fmt.Errorf("graph.Store.AllRefs: %w", err)
		}
		r.Kind = graph.EdgeKind(kind)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllEdges returns every edge, ordered by (from_id, to_id, kind).
func (s *Store) AllEdges() ([]graph.Edge, error) {
	rows, err := s.db.Query(`SELECT from_id,to_id,kind,confidence FROM edges ORDER BY from_id,to_id,kind`)
	if err != nil {
		return nil, fmt.Errorf("graph.Store.AllEdges: %w", err)
	}
	defer rows.Close()
	var out []graph.Edge
	for rows.Next() {
		var e graph.Edge
		var kind, conf string
		if err := rows.Scan(&e.From, &e.To, &kind, &conf); err != nil {
			return nil, fmt.Errorf("graph.Store.AllEdges: %w", err)
		}
		e.Kind, e.Confidence = graph.EdgeKind(kind), graph.Confidence(conf)
		out = append(out, e)
	}
	return out, rows.Err()
}
