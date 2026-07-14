package memory_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// wantSyncID re-derives the deterministic sync_id the store computes, so the
// test does not depend on the unexported computeSyncID.
func wantSyncID(scope, topic string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + topic))
	return "obs-" + hex.EncodeToString(sum[:])[:24]
}

func TestRescope_MovesScopeAndSyncIDPreservingIdentity(t *testing.T) {
	s := openTestStore(t)
	orig, err := s.Upsert("project/rag", "strategy text", memory.UpsertOpts{Author: "me@x"})
	if err != nil {
		t.Fatal(err)
	}
	moved, changed, err := s.Rescope("project/rag", "project", "personal", "")
	if err != nil || !changed {
		t.Fatalf("Rescope: err=%v changed=%v", err, changed)
	}
	if moved.Scope != "personal" {
		t.Errorf("scope = %q, want personal", moved.Scope)
	}
	if moved.SyncID != wantSyncID("personal", "project/rag") {
		t.Errorf("sync_id = %q, want recomputed for personal scope", moved.SyncID)
	}
	if moved.ID != orig.ID || moved.Revision != orig.Revision || moved.Content != "strategy text" {
		t.Error("identity (id/revision/content) not preserved")
	}
	// Personal scope is excluded from export (R1).
	recs, err := s.ExportRecords("me@x")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("rescoped-to-personal observation still exported (%d records)", len(recs))
	}
}

func TestRescope_CollisionLeavesBothRows(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("k", "a", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("k", "b", memory.UpsertOpts{Scope: "personal"}); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := s.Rescope("k", "project", "personal", ""); err == nil {
		t.Fatal("expected collision error rescoping project→personal when personal exists")
	} else if changed {
		t.Error("collision must not report a change")
	}
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("want 2 rows intact after failed rescope, got %d", len(obs))
	}
}

func TestRescope_NotFoundAndNoop(t *testing.T) {
	s := openTestStore(t)
	if _, changed, err := s.Rescope("missing", "project", "personal", ""); err != nil || changed {
		t.Errorf("not-found: changed=%v err=%v, want (false,nil)", changed, err)
	}
	if _, err := s.Upsert("k", "v", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	obs, changed, err := s.Rescope("k", "project", "project", "")
	if err != nil || changed {
		t.Errorf("from==to no-op: changed=%v err=%v, want (false,nil)", changed, err)
	}
	if obs.Scope != "project" {
		t.Errorf("no-op should return the existing row, got scope %q", obs.Scope)
	}
}

func TestDelete_SoftDeletes(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Upsert("k", "findable", memory.UpsertOpts{Author: "me@x"}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Delete("k", "project", "")
	if err != nil || !ok {
		t.Fatalf("Delete: err=%v ok=%v", err, ok)
	}
	if n := storeLen(t, s); n != 0 {
		t.Errorf("Len = %d after delete, want 0", n)
	}
	res, _, err := s.Search("findable", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Error("deleted observation still returned by Search")
	}
	recs, err := s.ExportRecords("me@x")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Error("deleted observation still exported")
	}
	ok2, err := s.Delete("k", "project", "")
	if err != nil || ok2 {
		t.Errorf("second delete: ok=%v err=%v, want (false,nil)", ok2, err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	s := openTestStore(t)
	if ok, err := s.Delete("missing", "project", ""); err != nil || ok {
		t.Errorf("not-found delete: ok=%v err=%v, want (false,nil)", ok, err)
	}
}
