package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
