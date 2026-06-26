package memory

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ChunkRecord is the on-disk, shareable form of an observation. It deliberately
// omits the local primary key (id): identity travels through sync_id, and each
// importing machine mints its own local id.
type ChunkRecord struct {
	SyncID    string `json:"sync_id"`
	TopicKey  string `json:"topic_key"`
	Scope     string `json:"scope"`
	Project   string `json:"project"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	Source    string `json:"source"`
	Author    string `json:"author"`
	Revision  int    `json:"revision"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// chunkFile is the JSON document stored (gzipped) in a chunk file.
type chunkFile struct {
	Observations []ChunkRecord `json:"observations"`
}

// ExportRecords returns this developer's authored live observations as chunk
// records, sorted by sync_id. Records with an empty author are stamped with the
// given author so ownership is well-defined after another machine imports them.
func (s *Store) ExportRecords(author string) ([]ChunkRecord, error) {
	rows, err := s.db.Query(`SELECT `+obsColumns+` FROM observations
		WHERE deleted_at IS NULL AND scope != 'personal' AND (author = ? OR author = '')
		ORDER BY sync_id`, author)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.ExportRecords: %w", err)
	}
	obs, err := collectRows(rows)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.ExportRecords: %w", err)
	}
	out := make([]ChunkRecord, 0, len(obs))
	for _, o := range obs {
		a := o.Author
		if a == "" {
			a = author
		}
		out = append(out, ChunkRecord{
			SyncID: o.SyncID, TopicKey: o.TopicKey, Scope: o.Scope, Project: o.Project,
			Title: o.Title, Content: o.Content, Type: o.Type, Source: o.Source, Author: a,
			Revision:  o.Revision,
			CreatedAt: o.CreatedAt.Format(timeFormat),
			UpdatedAt: o.UpdatedAt.Format(timeFormat),
		})
	}
	return out, nil
}

// ImportResult counts the outcome of an import.
type ImportResult struct {
	Inserted int
	Updated  int
	Skipped  int
}

// ImportRecords upserts records by sync_id. A record with a higher revision than
// the local row overwrites it; equal or lower revisions are skipped.
func (s *Store) ImportRecords(records []ChunkRecord) (ImportResult, error) {
	var res ImportResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("memory.Store.ImportRecords: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for _, r := range records {
		if r.SyncID == "" {
			res.Skipped++
			continue
		}
		created := parseChunkTime("created_at", r.CreatedAt, r.SyncID)
		updated := parseChunkTime("updated_at", r.UpdatedAt, r.SyncID)
		incoming := Observation{
			ID: newID(), TopicKey: r.TopicKey, Scope: r.Scope, Project: r.Project,
			Title: r.Title, Content: r.Content, Type: r.Type, Source: r.Source,
			Author: r.Author, SyncID: r.SyncID, Revision: r.Revision,
			CreatedAt: created, UpdatedAt: updated,
		}
		existing, found, err := findBySyncIDTx(tx, r.SyncID)
		if err != nil {
			return res, fmt.Errorf("memory.Store.ImportRecords: %w", err)
		}
		if !found {
			if err := insertTx(tx, incoming, s.fts); err != nil {
				return res, fmt.Errorf("memory.Store.ImportRecords: %w", err)
			}
			res.Inserted++
			continue
		}
		if r.Revision <= existing.Revision {
			res.Skipped++
			continue
		}
		incoming.ID = existing.ID
		if _, err := tx.Exec(`UPDATE observations
			SET topic_key=?, scope=?, project=?, title=?, content=?, type=?, source=?, author=?, revision=?, created_at=?, updated_at=?
			WHERE id=?`,
			incoming.TopicKey, incoming.Scope, incoming.Project, incoming.Title, incoming.Content,
			incoming.Type, incoming.Source, incoming.Author, incoming.Revision,
			incoming.CreatedAt.Format(timeFormat), incoming.UpdatedAt.Format(timeFormat),
			incoming.ID); err != nil {
			return res, fmt.Errorf("memory.Store.ImportRecords: update: %w", err)
		}
		if err := ftsUpdate(tx, s.fts, incoming); err != nil {
			return res, fmt.Errorf("memory.Store.ImportRecords: %w", err)
		}
		res.Updated++
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("memory.Store.ImportRecords: commit: %w", err)
	}
	return res, nil
}

// parseChunkTime parses a timestamp string from a chunk file. If parsing fails
// it falls back to the current UTC time so the observation is still imported
// with a sane timestamp, and emits a warning naming the offending sync_id and
// field so the operator can diagnose hand-edits or merge-tool corruption.
func parseChunkTime(field, value, syncID string) time.Time {
	t, err := time.Parse(timeParseFormat, value)
	if err != nil {
		t = time.Now().UTC()
		slog.Warn("memory.ImportRecords: malformed chunk timestamp; falling back to now",
			"sync_id", syncID, "field", field, "value", value)
	}
	return t
}

// findBySyncIDTx looks up a live observation by its sync_id.
func findBySyncIDTx(db execer, syncID string) (Observation, bool, error) {
	row := db.QueryRow(`SELECT `+obsColumns+` FROM observations
		WHERE sync_id = ? AND deleted_at IS NULL`, syncID)
	o, err := scanObservation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, false, nil
	}
	if err != nil {
		return Observation{}, false, fmt.Errorf("memory.findBySyncID: %w", err)
	}
	return o, true, nil
}

// authorSlug turns a git author string into a filesystem-safe, stable slug.
func authorSlug(author string) string {
	author = strings.ToLower(strings.TrimSpace(author))
	if author == "" {
		return "local"
	}
	var b strings.Builder
	for _, r := range author {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "local"
	}
	return slug
}

// canonicalChunkJSON marshals records sorted by sync_id, so identical content
// always serializes to identical bytes.
func canonicalChunkJSON(records []ChunkRecord) ([]byte, error) {
	sorted := append([]ChunkRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SyncID < sorted[j].SyncID })
	data, err := json.MarshalIndent(chunkFile{Observations: sorted}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("memory.canonicalChunkJSON: %w", err)
	}
	return data, nil
}

// WriteChunk writes this author's records to dir/chunk-<slug>.jsonl.gz. It is
// idempotent: if the file already holds identical records, nothing is written
// (written=false) and the bytes are left untouched, so git sees no change.
func WriteChunk(dir, author string, records []ChunkRecord) (string, bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("memory.WriteChunk: mkdir: %w", err)
	}
	path := filepath.Join(dir, "chunk-"+authorSlug(author)+".jsonl.gz")
	want, err := canonicalChunkJSON(records)
	if err != nil {
		return "", false, err
	}
	if existing, err := readChunkRaw(path); err == nil && bytes.Equal(existing, want) {
		return path, false, nil
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf) // ModTime left zero → deterministic header bytes
	if _, err := gw.Write(want); err != nil {
		return "", false, fmt.Errorf("memory.WriteChunk: gzip: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", false, fmt.Errorf("memory.WriteChunk: gzip close: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", false, fmt.Errorf("memory.WriteChunk: write: %w", err)
	}
	return path, true, nil
}

// readChunkRaw returns the decompressed JSON bytes of a chunk file.
func readChunkRaw(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("memory.readChunkRaw: gzip: %w", err)
	}
	defer gr.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(gr); err != nil {
		return nil, fmt.Errorf("memory.readChunkRaw: read: %w", err)
	}
	return out.Bytes(), nil
}

// ReadAllChunks reads every chunk file in dir and returns the concatenated
// records, sorted by sync_id. A missing dir yields no records and no error.
func ReadAllChunks(dir string) ([]ChunkRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory.ReadAllChunks: %w", err)
	}
	var all []ChunkRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "chunk-") || !strings.HasSuffix(e.Name(), ".jsonl.gz") {
			continue
		}
		raw, err := readChunkRaw(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("memory.ReadAllChunks: %s: %w", e.Name(), err)
		}
		var cf chunkFile
		if err := json.Unmarshal(raw, &cf); err != nil {
			return nil, fmt.Errorf("memory.ReadAllChunks: %s: %w", e.Name(), err)
		}
		all = append(all, cf.Observations...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].SyncID < all[j].SyncID })
	return all, nil
}
