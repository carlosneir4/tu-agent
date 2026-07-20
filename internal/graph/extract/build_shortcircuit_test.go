package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// "Sentinel edge": an edge row inserted directly into the store by the test,
// which no source file produces. If a build rewrites the resolved edge table
// (calls ReplaceResolvedEdges), the sentinel vanishes; if the build skipped
// resolve, it survives. This makes "resolve did not run" directly observable.

const shortCircuitMainGo = `package main

func Run() {}
`

const shortCircuitExtraGo = `package main

func Helper() {}
`

// sentinelEdge is a resolved edge that no parser produces, used to detect
// whether a build called ReplaceResolvedEdges.
var sentinelEdge = graph.Edge{
	From:       "sentinel::from",
	To:         "sentinel::to",
	Kind:       graph.EdgeKind("sentinel"),
	Confidence: graph.ConfExact,
}

// insertSentinelEdge writes sentinelEdge directly into st's resolved edge
// table, on top of whatever edges already resolved there.
func insertSentinelEdge(t *testing.T, st *store.Store) {
	t.Helper()
	existing, err := st.AllEdges()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	if err := st.ReplaceResolvedEdges(append(existing, sentinelEdge), nil); err != nil {
		t.Fatalf("ReplaceResolvedEdges (planting sentinel): %v", err)
	}
}

// hasSentinelEdge reports whether sentinelEdge is still present in st.
func hasSentinelEdge(t *testing.T, st *store.Store) bool {
	t.Helper()
	edges, err := st.AllEdges()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	for _, e := range edges {
		if e.From == sentinelEdge.From && e.To == sentinelEdge.To && e.Kind == sentinelEdge.Kind {
			return true
		}
	}
	return false
}

// @s1: a no-op rebuild (nothing changed since the last build) must leave the
// resolved edge table byte-for-byte untouched — resolve must not run.
func TestBuildNoOpSkipsResolve(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "main.go", shortCircuitMainGo)

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".go"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	insertSentinelEdge(t, st)

	res, err := Build(dir, []string{".go"}, st)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if res.Parsed != 0 || res.Deleted != 0 {
		t.Fatalf("expected a no-op build (Parsed=0, Deleted=0), got %+v", res)
	}
	if !hasSentinelEdge(t, st) {
		t.Error("sentinel edge missing after a no-op build: resolve ran (rewrote the edge table) when it should have been skipped")
	}
}

// @s2: a rebuild after a source file edit must still resolve — the sentinel
// is gone because ReplaceResolvedEdges ran again.
func TestBuildEditedFileStillResolves(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "main.go", shortCircuitMainGo)

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".go"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	insertSentinelEdge(t, st)

	// Modify the file's content (different size/content, so the stat/SHA
	// fast-path cannot treat it as unchanged).
	writeFixture(t, dir, "main.go", shortCircuitMainGo+"\nfunc Extra() {}\n")

	res, err := Build(dir, []string{".go"}, st)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if res.Parsed < 1 {
		t.Fatalf("expected at least 1 parsed file after an edit, got %+v", res)
	}
	if hasSentinelEdge(t, st) {
		t.Error("sentinel edge survived a build that parsed a changed file: resolve should have rewritten the edge table")
	}
}

// @s3: a rebuild after a source file deletion must still resolve — the
// sentinel is gone because ReplaceResolvedEdges ran again.
func TestBuildDeletedFileStillResolves(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "main.go", shortCircuitMainGo)
	writeFixture(t, dir, "extra.go", shortCircuitExtraGo)

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".go"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	insertSentinelEdge(t, st)

	if err := os.Remove(filepath.Join(dir, "extra.go")); err != nil {
		t.Fatalf("remove extra.go: %v", err)
	}

	res, err := Build(dir, []string{".go"}, st)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if res.Deleted != 1 {
		t.Fatalf("expected 1 deleted file, got %+v", res)
	}
	if hasSentinelEdge(t, st) {
		t.Error("sentinel edge survived a build that deleted a file: resolve should have rewritten the edge table")
	}
}
