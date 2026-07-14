package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestRelinkObservations_DerivesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Seed a graph with one class node named OrderService.
	if err := os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gs, err := store.Open(filepath.Join(dir, ".tu-agent", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("graph open: %v", err)
	}
	if err := gs.ReplaceFileNodes("src/OrderService.java",
		[]graph.Node{{ID: "src/OrderService.java::OrderService", Kind: graph.KindClass, Name: "OrderService", Path: "src/OrderService.java"}},
		nil, nil); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	_ = gs.Close()

	// Seed a note that names OrderService in prose + a manual link to another node.
	ms, err := memory.Open(memoryDBPath(dir))
	if err != nil {
		t.Fatalf("mem open: %v", err)
	}
	o, err := ms.Upsert("bug-pattern/x", "OrderService loses state on retry", memory.UpsertOpts{Type: "bug-pattern"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Relate(o.ID, "src/Other.java::Other", "documents"); err != nil {
		t.Fatal(err)
	}
	_ = ms.Close()

	// Relink twice — must be idempotent (no duplicate auto links).
	var buf strings.Builder
	if err := relinkObservations(&buf, true); err != nil {
		t.Fatalf("relink 1: %v", err)
	}
	if err := relinkObservations(&buf, true); err != nil {
		t.Fatalf("relink 2: %v", err)
	}

	ms, _ = memory.Open(memoryDBPath(dir))
	defer ms.Close()
	rels, err := ms.RelationsFrom([]string{o.ID})
	if err != nil {
		t.Fatal(err)
	}
	var auto, manual int
	for _, r := range rels {
		switch r.Type {
		case "documents_auto":
			auto++
			if r.ToID != "src/OrderService.java::OrderService" {
				t.Errorf("auto link to wrong node: %s", r.ToID)
			}
		case "documents":
			manual++
		}
	}
	if auto != 1 {
		t.Errorf("auto links = %d, want 1 (idempotent)", auto)
	}
	if manual != 1 {
		t.Errorf("manual link clobbered: %d, want 1", manual)
	}
}

func TestBuildNameIndex_UniqueClassFileOnly(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a/OrderService.java::OrderService", Kind: graph.KindClass, Name: "OrderService"},
		{ID: "a/OrderService.java", Kind: graph.KindFile, Name: "OrderService.java"},
		{ID: "x/Dup.java::Dup", Kind: graph.KindClass, Name: "Dup"},
		{ID: "y/Dup.java::Dup", Kind: graph.KindClass, Name: "Dup"},            // collision -> omitted
		{ID: "f/foo.java::compute", Kind: graph.KindFunction, Name: "compute"}, // function -> ignored
	}
	idx := buildNameIndex(nodes)
	if idx["OrderService"] != "a/OrderService.java::OrderService" {
		t.Errorf("OrderService not indexed: %v", idx)
	}
	if _, ok := idx["Dup"]; ok {
		t.Errorf("colliding name Dup must be omitted")
	}
	if _, ok := idx["compute"]; ok {
		t.Errorf("function names must be ignored")
	}
}
