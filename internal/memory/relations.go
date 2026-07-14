package memory

import (
	"fmt"
	"strings"
	"time"
)

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
	id, err := newID()
	if err != nil {
		return Relation{}, fmt.Errorf("memory.Store.Relate: %w", err)
	}
	rel := Relation{ID: id, FromID: fromID, ToID: toID, Type: relType, CreatedAt: now}
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
