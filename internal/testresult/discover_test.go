package testresult

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeReport(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAndLoad(t *testing.T) {
	root := t.TempDir()
	writeReport(t, filepath.Join(root, "build", "test-results", "test", "a.xml"), sampleJUnit)
	writeReport(t, filepath.Join(root, "target", "surefire-reports", "b.xml"), sampleJUnit)

	paths, err := DiscoverJUnitReports(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("discovered %d reports, want 2: %v", len(paths), paths)
	}

	rep, err := LoadReports(root, time.Time{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rep.Cases) != 6 { // 3 per file
		t.Fatalf("loaded %d cases, want 6", len(rep.Cases))
	}
}

func TestLoadReportsStaleFilter(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "build", "test-results", "test", "old.xml")
	writeReport(t, stale, sampleJUnit)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	// since = now: the stale report is excluded.
	rep, err := LoadReports(root, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rep.Cases) != 0 {
		t.Fatalf("stale report included: %d cases", len(rep.Cases))
	}
}
