package memory_test

import (
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestExportRecordsExcludesPersonalScope(t *testing.T) {
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := s.Upsert("architecture/shared", "team-visible note", memory.UpsertOpts{Author: "dev@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("private/secret-plan", "do not share", memory.UpsertOpts{Author: "dev@example.com", Scope: "personal"}); err != nil {
		t.Fatal(err)
	}

	recs, err := s.ExportRecords("dev@example.com")
	if err != nil {
		t.Fatalf("ExportRecords: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ExportRecords returned %d records, want 1 (personal excluded)", len(recs))
	}
	if recs[0].TopicKey != "architecture/shared" {
		t.Errorf("exported the wrong record: %q (personal scope leaked?)", recs[0].TopicKey)
	}
	if recs[0].Scope == "personal" {
		t.Error("a personal-scoped record was exported")
	}
}
