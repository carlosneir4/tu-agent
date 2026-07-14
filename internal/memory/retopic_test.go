package memory_test

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// @s1: Retopic renames the topic_key in place, preserving id/created_at/content
// and relations while bumping the revision and advancing updated_at.
func TestRetopic_RenamesInPlacePreservingIdentityAndRelations(t *testing.T) {
	s := openTestStore(t)
	orig, err := s.Upsert("skill/old", "body", memory.UpsertOpts{Author: "me@x"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Upsert("reference/other", "other body", memory.UpsertOpts{Author: "me@x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Relate(orig.ID, other.ID, "documents"); err != nil {
		t.Fatal(err)
	}

	row, changed, err := s.Retopic("skill/old", "skill/new", "project", "")
	if err != nil || !changed {
		t.Fatalf("Retopic: err=%v changed=%v, want (true,nil)", err, changed)
	}
	if row.TopicKey != "skill/new" {
		t.Errorf("topic_key = %q, want skill/new", row.TopicKey)
	}
	if row.SyncID != wantSyncID("project", "skill/new") {
		t.Errorf("sync_id = %q, want recomputed for skill/new", row.SyncID)
	}
	if row.Revision != orig.Revision+1 {
		t.Errorf("revision = %d, want %d (orig+1)", row.Revision, orig.Revision+1)
	}
	if row.ID != orig.ID {
		t.Errorf("id = %q, want preserved %q", row.ID, orig.ID)
	}
	if !row.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("created_at = %v, want preserved %v", row.CreatedAt, orig.CreatedAt)
	}
	if row.Content != "body" {
		t.Errorf("content = %q, want body", row.Content)
	}
	if row.UpdatedAt.Before(orig.UpdatedAt) {
		t.Errorf("updated_at %v regressed below orig %v", row.UpdatedAt, orig.UpdatedAt)
	}

	// The relation survives: id unchanged means it was an in-place UPDATE, not
	// delete+insert (which would orphan the relation on a new id).
	rels, err := s.RelationsFrom([]string{orig.ID})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rels {
		if r.FromID == orig.ID && r.ToID == other.ID && r.Type == "documents" {
			found = true
		}
	}
	if !found {
		t.Errorf("relation from %s did not survive retopic (in-place UPDATE not preserved)", orig.ID)
	}

	// Exactly one row for this record, keyed by skill/new; nothing left at skill/old.
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var newCount, oldCount int
	for _, o := range obs {
		switch o.TopicKey {
		case "skill/new":
			newCount++
		case "skill/old":
			oldCount++
		}
	}
	if newCount != 1 {
		t.Errorf("skill/new rows = %d, want 1", newCount)
	}
	if oldCount != 0 {
		t.Errorf("skill/old rows = %d, want 0 (renamed away)", oldCount)
	}
}

// @s2: A retopic onto an occupied target key is rejected with an error and
// leaves both rows unmutated.
func TestRetopic_CollisionRejectedWithoutMutation(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("skill/old", "a", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("skill/new", "b", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}

	if _, changed, err := s.Retopic("skill/old", "skill/new", "project", ""); err == nil {
		t.Fatal("expected collision error retopicing skill/old→skill/new when skill/new exists")
	} else if changed {
		t.Error("collision must not report a change")
	}

	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]string{}
	for _, o := range obs {
		byKey[o.TopicKey] = o.Content
	}
	if byKey["skill/old"] != "a" {
		t.Errorf("skill/old content = %q, want unchanged a", byKey["skill/old"])
	}
	if byKey["skill/new"] != "b" {
		t.Errorf("skill/new content = %q, want unchanged b", byKey["skill/new"])
	}
}

// @s3: An absent source key is a zero-value no-op that creates nothing, and a
// same-key retopic returns the existing row without a change.
func TestRetopic_AbsentAndSameKeyNoop(t *testing.T) {
	s := openTestStore(t)

	row, changed, err := s.Retopic("skill/missing", "skill/whatever", "project", "")
	if err != nil || changed {
		t.Errorf("absent source: changed=%v err=%v, want (false,nil)", changed, err)
	}
	if row != (memory.Observation{}) {
		t.Errorf("absent source: row = %+v, want zero value", row)
	}
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 0 {
		t.Fatalf("absent-source retopic created rows: got %d, want 0", len(obs))
	}

	if _, err := s.Upsert("skill/k", "v", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	same, changed, err := s.Retopic("skill/k", "skill/k", "project", "")
	if err != nil || changed {
		t.Errorf("same-key no-op: changed=%v err=%v, want (false,nil)", changed, err)
	}
	if same.TopicKey != "skill/k" {
		t.Errorf("same-key no-op should return existing row, got topic_key %q", same.TopicKey)
	}
}
