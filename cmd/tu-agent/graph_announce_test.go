package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// mustInitGraphFixture creates a .git marker (so repoRoot() anchors at dir)
// and opens the graph store to create .tu-agent/graph.db, then closes it.
func mustInitGraphFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	s, err := openGraphStore()
	if err != nil {
		t.Fatalf("openGraphStore: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close graph store: %v", err)
	}
}

func TestAnnounceGraph_NoGraphIsSilentNoop(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph without graph.db: %v", err)
	}
	if out != "" {
		t.Errorf("expected no output without a graph, got %q", out)
	}
}

func TestGraphEmptyWarning(t *testing.T) {
	if w := graphEmptyWarning(0, 5); !strings.Contains(w, "EMPTY") || !strings.Contains(w, "learn") {
		t.Errorf("empty graph (0 nodes, 5 files) should warn to run learn; got %q", w)
	}
	if w := graphEmptyWarning(10, 5); w != "" {
		t.Errorf("healthy graph should not warn; got %q", w)
	}
	if w := graphEmptyWarning(0, 0); w != "" {
		t.Errorf("unbuilt graph (no files) should not warn; got %q", w)
	}
}

// mustSeedFileRow opens the graph store and writes one files row with no nodes —
// the silent-empty-graph state (files present, zero nodes).
func mustSeedFileRow(t *testing.T) {
	t.Helper()
	s, err := openGraphStore()
	if err != nil {
		t.Fatalf("openGraphStore: %v", err)
	}
	if err := s.UpsertFile(store.FileRecord{Path: "a.go", SHA256: "x", Language: "go", Status: "ok"}); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAnnounceGraph_WarnsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInitGraphFixture(t, dir)
	mustSeedFileRow(t)

	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph: %v", err)
	}
	if !strings.Contains(out, "EMPTY") || !strings.Contains(out, "tu-agent learn") {
		t.Errorf("expected loud empty-graph warning; got:\n%s", out)
	}
	if strings.Contains(out, "graph ready") {
		t.Errorf("empty graph must not report 'graph ready'; got:\n%s", out)
	}
}

func TestAnnounceGraph_PrintsNudgeWithCounts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInitGraphFixture(t, dir)

	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph: %v", err)
	}
	for _, want := range []string{
		"graph ready",
		"get_context",
		"DEFERRED",
		"ToolSearch",
		"tu-agent graph context",
		"mem_recent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("nudge missing %q; got:\n%s", want, out)
		}
	}
}
