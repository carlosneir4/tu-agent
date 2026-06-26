package store

import (
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

func TestOpenCreatesSchemaAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	got, err := s.Meta("extractor_version")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if got != "v1" {
		t.Errorf("extractor_version = %q, want v1", got)
	}
}

func TestOpenRebuildsOnVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open v1: %v", err)
	}
	if err := s.UpsertFile(FileRecord{Path: "a.java", SHA256: "x", Language: "java", Status: "ok"}); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	s.Close()

	s2, err := Open(path, "v2") // version bump → silent full rebuild
	if err != nil {
		t.Fatalf("Open v2: %v", err)
	}
	defer s2.Close()
	files, err := s2.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty store after version rebuild, got %d files", len(files))
	}
	if v, _ := s2.Meta("extractor_version"); v != "v2" {
		t.Errorf("extractor_version = %q, want v2", v)
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "graph.db"), "test")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReplaceFileNodesIsIdempotentPerFile(t *testing.T) {
	s := openTest(t)
	nodes := []graph.Node{
		{ID: "a.java", Kind: graph.KindFile, Name: "a.java", Path: "a.java", Language: "java"},
		{ID: "a.java::A", Kind: graph.KindClass, Name: "A", Path: "a.java", Line: 1, EndLine: 3, Language: "java"},
	}
	refs := []graph.Ref{{FromID: "a.java::A", Kind: graph.EdgeExtends, Name: "Base", Line: 1}}
	contains := []graph.Edge{{From: "a.java", To: "a.java::A", Kind: graph.EdgeContains, Confidence: graph.ConfExact}}
	if err := s.ReplaceFileNodes("a.java", nodes, refs, contains); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	// Replacing again with one node must leave exactly that node.
	if err := s.ReplaceFileNodes("a.java", nodes[:1], nil, nil); err != nil {
		t.Fatalf("ReplaceFileNodes 2nd: %v", err)
	}
	got, err := s.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a.java" {
		t.Errorf("after replace, nodes = %+v, want only a.java", got)
	}
	refsGot, err := s.AllRefs()
	if err != nil {
		t.Fatalf("AllRefs: %v", err)
	}
	if len(refsGot) != 0 {
		t.Errorf("refs not cleared on replace: %+v", refsGot)
	}
}

func TestDeleteFileRemovesNodesRefsAndEdges(t *testing.T) {
	s := openTest(t)
	nodes := []graph.Node{{ID: "b.java", Kind: graph.KindFile, Name: "b.java", Path: "b.java", Language: "java"}}
	if err := s.ReplaceFileNodes("b.java", nodes, nil, nil); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	if err := s.UpsertFile(FileRecord{Path: "b.java", SHA256: "x", Language: "java", Status: "ok"}); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if err := s.DeleteFile("b.java"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	got, _ := s.AllNodes()
	if len(got) != 0 {
		t.Errorf("nodes remain after DeleteFile: %+v", got)
	}
	files, _ := s.Files()
	if len(files) != 0 {
		t.Errorf("files row remains after DeleteFile")
	}
}

func TestReplaceResolvedEdgesKeepsContains(t *testing.T) {
	s := openTest(t)
	contains := []graph.Edge{{From: "a.java", To: "a.java::A", Kind: graph.EdgeContains, Confidence: graph.ConfExact}}
	nodes := []graph.Node{{ID: "a.java", Kind: graph.KindFile, Name: "a.java", Path: "a.java"}}
	if err := s.ReplaceFileNodes("a.java", nodes, nil, contains); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	resolved := []graph.Edge{{From: "a.java::A", To: "c.java::Base", Kind: graph.EdgeExtends, Confidence: graph.ConfExact}}
	if err := s.ReplaceResolvedEdges(resolved, nil); err != nil {
		t.Fatalf("ReplaceResolvedEdges: %v", err)
	}
	if err := s.ReplaceResolvedEdges(nil, nil); err != nil { // re-resolve to empty
		t.Fatalf("ReplaceResolvedEdges empty: %v", err)
	}
	edges, err := s.AllEdges()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].Kind != graph.EdgeContains {
		t.Errorf("edges = %+v, want only the contains edge", edges)
	}
}

func TestFilesRoundTrip(t *testing.T) {
	s := openTest(t)
	f := FileRecord{
		Path:     "pkg/Foo.java",
		SHA256:   "abc123",
		Language: "java",
		Status:   "ok",
		Package:  "pkg",
		Imports:  []string{"java.util.List", "java.io.File"},
	}
	if err := s.UpsertFile(f); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	files, err := s.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	got, ok := files["pkg/Foo.java"]
	if !ok {
		t.Fatal("pkg/Foo.java not found in Files()")
	}
	if got.SHA256 != "abc123" || got.Language != "java" || got.Package != "pkg" {
		t.Errorf("unexpected FileRecord: %+v", got)
	}
	if len(got.Imports) != 2 || got.Imports[0] != "java.util.List" {
		t.Errorf("unexpected Imports: %+v", got.Imports)
	}
}

func TestAllRefsRoundTrip(t *testing.T) {
	s := openTest(t)
	nodes := []graph.Node{
		{ID: "r.java::R", Kind: graph.KindClass, Name: "R", Path: "r.java", Language: "java"},
	}
	refs := []graph.Ref{
		{FromID: "r.java::R", Kind: graph.EdgeExtends, Name: "Base", Line: 5},
		{FromID: "r.java::R", Kind: graph.EdgeCalls, Name: "helper", Line: 10},
	}
	if err := s.ReplaceFileNodes("r.java", nodes, refs, nil); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	got, err := s.AllRefs()
	if err != nil {
		t.Fatalf("AllRefs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("AllRefs: want 2, got %d: %+v", len(got), got)
	}
}

func TestOpenReopenSameVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path, "v1")
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	s.Close()
	s2, err := Open(path, "v1") // same version → reuse
	if err != nil {
		t.Fatalf("Open second same version: %v", err)
	}
	defer s2.Close()
	v, err := s2.Meta("extractor_version")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if v != "v1" {
		t.Errorf("extractor_version = %q, want v1", v)
	}
}

func TestReplaceKnowledgeRoundTrips(t *testing.T) {
	s := openTest(t)
	nodes := []graph.Node{
		{ID: "skill::billing", Kind: graph.KindSkill, Name: "billing", Path: ".claude/skills/billing/SKILL.md", Line: 1},
		{ID: "convention::project", Kind: graph.KindConvention, Name: "conventions", Path: "CLAUDE.md", Line: 10, EndLine: 30},
	}
	edges := []graph.Edge{{From: "skill::billing", To: "a.java", Kind: graph.EdgeDocuments, Confidence: graph.ConfHigh}}
	if err := s.ReplaceKnowledge(nodes, edges); err != nil {
		t.Fatalf("ReplaceKnowledge: %v", err)
	}
	got, _ := s.AllNodes()
	var skills, conv int
	for _, n := range got {
		switch n.Kind {
		case graph.KindSkill:
			skills++
		case graph.KindConvention:
			conv++
		}
	}
	if skills != 1 || conv != 1 {
		t.Errorf("nodes after ReplaceKnowledge: skills=%d conv=%d, want 1/1", skills, conv)
	}
	// Replacing again with nothing must clear knowledge nodes and documents edges.
	if err := s.ReplaceKnowledge(nil, nil); err != nil {
		t.Fatalf("ReplaceKnowledge empty: %v", err)
	}
	got, _ = s.AllNodes()
	for _, n := range got {
		if n.Kind == graph.KindSkill || n.Kind == graph.KindConvention {
			t.Errorf("knowledge node not cleared: %+v", n)
		}
	}
	edgesGot, _ := s.AllEdges()
	for _, e := range edgesGot {
		if e.Kind == graph.EdgeDocuments {
			t.Errorf("documents edge not cleared: %+v", e)
		}
	}
}

func TestReplaceResolvedEdgesPreservesKnowledge(t *testing.T) {
	s := openTest(t)
	// A contains edge (preserved by resolve) and a documents edge (must also survive).
	contains := []graph.Edge{{From: "a.java", To: "a.java::A", Kind: graph.EdgeContains, Confidence: graph.ConfExact}}
	if err := s.ReplaceFileNodes("a.java", []graph.Node{{ID: "a.java", Kind: graph.KindFile, Name: "a.java", Path: "a.java"}}, nil, contains); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	if err := s.ReplaceKnowledge(
		[]graph.Node{{ID: "skill::x", Kind: graph.KindSkill, Name: "x", Path: "s.md"}},
		[]graph.Edge{{From: "skill::x", To: "a.java", Kind: graph.EdgeDocuments, Confidence: graph.ConfHigh}},
	); err != nil {
		t.Fatalf("ReplaceKnowledge: %v", err)
	}
	// Re-resolving code edges must NOT drop the documents edge.
	if err := s.ReplaceResolvedEdges(nil, nil); err != nil {
		t.Fatalf("ReplaceResolvedEdges: %v", err)
	}
	edges, _ := s.AllEdges()
	var hasContains, hasDocuments bool
	for _, e := range edges {
		switch e.Kind {
		case graph.EdgeContains:
			hasContains = true
		case graph.EdgeDocuments:
			hasDocuments = true
		}
	}
	if !hasContains || !hasDocuments {
		t.Errorf("after resolve: contains=%v documents=%v, want both true", hasContains, hasDocuments)
	}
}

func TestReplaceResolvedEdgesPersistsExternalNodes(t *testing.T) {
	s := openTest(t)
	stub := graph.Node{ID: "external::com.framework.Base", Kind: graph.KindExternal, Name: "com.framework.Base"}
	edges := []graph.Edge{
		{From: "a.java::A", To: "external::com.framework.Base", Kind: graph.EdgeExtends, Confidence: graph.ConfExact},
	}
	if err := s.ReplaceResolvedEdges(edges, []graph.Node{stub}); err != nil {
		t.Fatalf("ReplaceResolvedEdges: %v", err)
	}
	nodes, err := s.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "external::com.framework.Base" && n.Kind == graph.KindExternal {
			found = true
		}
	}
	if !found {
		t.Errorf("external stub node was not persisted; nodes: %+v", nodes)
	}
}

func TestUpsertFileRoundTripsSize(t *testing.T) {
	s := openTest(t)
	if err := s.UpsertFile(FileRecord{Path: "a.java", SHA256: "x", Language: "java", Status: "ok", Size: 1234}); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	files, _ := s.Files()
	if files["a.java"].Size != 1234 {
		t.Errorf("Size = %d, want 1234", files["a.java"].Size)
	}
}

func TestStorePersistsSignatures(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(dbPath, "vtest")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	n := graph.Node{
		ID: "billing/svc.go::Service.Process", Kind: graph.KindFunction,
		Name: "Service.Process", Path: "billing/svc.go", Line: 5, EndLine: 9,
		Language: "go", Params: "(invoice Invoice, force bool)", ReturnType: "(int, error)",
	}
	if err := s.ReplaceFileNodes("billing/svc.go", []graph.Node{n}, nil, nil); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}

	nodes, err := s.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	var got *graph.Node
	for i := range nodes {
		if nodes[i].ID == n.ID {
			got = &nodes[i]
		}
	}
	if got == nil {
		t.Fatalf("node not found in %+v", nodes)
	}
	if got.Params != n.Params || got.ReturnType != n.ReturnType {
		t.Errorf("signature = %q / %q, want %q / %q", got.Params, got.ReturnType, n.Params, n.ReturnType)
	}
}

func TestStore_ExportedRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "g.db"), "vtest")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	nodes := []graph.Node{
		{ID: "a.go::Pub", Kind: graph.KindFunction, Name: "Pub", Path: "a.go",
			Line: 1, EndLine: 5, Language: "go", Exported: true},
		{ID: "a.go::priv", Kind: graph.KindFunction, Name: "priv", Path: "a.go",
			Line: 7, EndLine: 11, Language: "go", Exported: false},
	}
	if err := s.ReplaceFileNodes("a.go", nodes, nil, nil); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}
	got, err := s.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	exported := map[string]bool{}
	for _, n := range got {
		exported[n.ID] = n.Exported
	}
	if !exported["a.go::Pub"] {
		t.Errorf("a.go::Pub Exported = false, want true")
	}
	if exported["a.go::priv"] {
		t.Errorf("a.go::priv Exported = true, want false")
	}
}

func TestConceptsRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "g.db"), "test-ext")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	rows := []ConceptRow{
		{Name: "widgets", Description: "widget rendering", Content: "---\nname: widgets\n---\nbody A"},
		{Name: "permalink", Description: "url building", Content: "---\nname: permalink\n---\nbody B"},
	}
	if err := s.ReplaceConcepts(rows); err != nil {
		t.Fatalf("ReplaceConcepts: %v", err)
	}

	got, ok, err := s.GetConcept("widgets")
	if err != nil || !ok {
		t.Fatalf("GetConcept(widgets): ok=%v err=%v", ok, err)
	}
	if got.Content != "---\nname: widgets\n---\nbody A" || got.Description != "widget rendering" {
		t.Errorf("GetConcept(widgets) = %+v", got)
	}

	if _, ok, _ := s.GetConcept("missing"); ok {
		t.Errorf("GetConcept(missing) ok = true, want false")
	}

	list, err := s.ListConcepts()
	if err != nil {
		t.Fatalf("ListConcepts: %v", err)
	}
	if len(list) != 2 || list[0].Name != "permalink" || list[1].Name != "widgets" {
		t.Errorf("ListConcepts not sorted/complete: %+v", list)
	}

	// Replace is wholesale: a smaller set drops the previous rows.
	if err := s.ReplaceConcepts([]ConceptRow{{Name: "only", Description: "d", Content: "c"}}); err != nil {
		t.Fatalf("ReplaceConcepts 2: %v", err)
	}
	list, err = s.ListConcepts()
	if err != nil {
		t.Fatalf("ListConcepts 2: %v", err)
	}
	if len(list) != 1 || list[0].Name != "only" {
		t.Errorf("wholesale replace failed: %+v", list)
	}
}

func TestUpsertConcept(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "g.db"), "test-ext")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.ReplaceConcepts([]ConceptRow{
		{Name: "a", Description: "da", Content: "ca"},
		{Name: "b", Description: "db", Content: "cb"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Update only "a"; "b" must be untouched.
	if err := s.UpsertConcept(ConceptRow{Name: "a", Description: "DA2", Content: "CA2"}); err != nil {
		t.Fatalf("UpsertConcept: %v", err)
	}
	got, ok, _ := s.GetConcept("a")
	if !ok || got.Description != "DA2" || got.Content != "CA2" {
		t.Errorf("a not updated: %+v ok=%v", got, ok)
	}
	other, ok, _ := s.GetConcept("b")
	if !ok || other.Description != "db" {
		t.Errorf("b should be untouched: %+v ok=%v", other, ok)
	}
	if list, _ := s.ListConcepts(); len(list) != 2 {
		t.Errorf("row count changed: %d, want 2", len(list))
	}
	// Upsert of a new name inserts.
	if err := s.UpsertConcept(ConceptRow{Name: "c", Description: "dc", Content: "cc"}); err != nil {
		t.Fatalf("UpsertConcept new: %v", err)
	}
	if _, ok, _ := s.GetConcept("c"); !ok {
		t.Errorf("c not inserted")
	}
}

func TestNodeCountAndExistingNodeIDs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "g.db"), "test-ev")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	nodes := []graph.Node{
		{ID: "svc.go", Kind: graph.KindFile, Name: "svc.go", Path: "svc.go", Language: "go"},
		{ID: "svc.go::Pay", Kind: graph.KindFunction, Name: "Pay", Path: "svc.go", Language: "go", Line: 1, EndLine: 5},
	}
	if err := s.ReplaceFileNodes("svc.go", nodes, nil, nil); err != nil {
		t.Fatal(err)
	}

	n, err := s.NodeCount()
	if err != nil || n != 2 {
		t.Fatalf("NodeCount = %d, err %v; want 2", n, err)
	}

	got, err := s.ExistingNodeIDs([]string{"svc.go::Pay", "svc.go::Gone", "other::X"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["svc.go::Pay"] || got["svc.go::Gone"] || got["other::X"] {
		t.Fatalf("ExistingNodeIDs = %v; want only svc.go::Pay live", got)
	}
	if empty, err := s.ExistingNodeIDs(nil); err != nil || len(empty) != 0 {
		t.Fatalf("ExistingNodeIDs(nil) = %v, err %v; want empty", empty, err)
	}
}

// TestReplaceResolvedEdgesClearsStaleOnTypeEdge is the regression test for the
// D2 defect: on_type (and spreads) edges were missing from the DELETE allow-list
// in ReplaceResolvedEdges, so stale edges survived an incremental re-resolve.
//
// Scenario: fragment CardFrag is re-mapped from Article → Author. After the
// second ReplaceResolvedEdges call, exactly one on_type edge must remain and it
// must point to Author, not Article.
func TestReplaceResolvedEdgesClearsStaleOnTypeEdge(t *testing.T) {
	s := openTest(t)

	// Insert a node for the fragment so usage mirrors the real extract pipeline.
	fragNode := graph.Node{
		ID:       "query.graphql::CardFrag",
		Kind:     graph.KindFunction,
		Name:     "CardFrag",
		Path:     "query.graphql",
		Language: "graphql",
	}
	if err := s.ReplaceFileNodes("query.graphql", []graph.Node{fragNode}, nil, nil); err != nil {
		t.Fatalf("ReplaceFileNodes: %v", err)
	}

	// Round 1: CardFrag is defined on Article.
	round1 := []graph.Edge{
		{From: "query.graphql::CardFrag", To: "schema.graphql::Article", Kind: graph.EdgeOnType, Confidence: graph.ConfExact},
	}
	if err := s.ReplaceResolvedEdges(round1, nil); err != nil {
		t.Fatalf("ReplaceResolvedEdges round1: %v", err)
	}

	// Round 2: CardFrag is now on Author — the stale Article edge must be gone.
	round2 := []graph.Edge{
		{From: "query.graphql::CardFrag", To: "schema.graphql::Author", Kind: graph.EdgeOnType, Confidence: graph.ConfExact},
	}
	if err := s.ReplaceResolvedEdges(round2, nil); err != nil {
		t.Fatalf("ReplaceResolvedEdges round2: %v", err)
	}

	edges, err := s.AllEdges()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	var onTypeEdges []graph.Edge
	for _, e := range edges {
		if e.Kind == graph.EdgeOnType {
			onTypeEdges = append(onTypeEdges, e)
		}
	}
	if len(onTypeEdges) != 1 {
		t.Fatalf("want exactly 1 on_type edge after re-resolve, got %d: %+v", len(onTypeEdges), onTypeEdges)
	}
	if onTypeEdges[0].To != "schema.graphql::Author" {
		t.Errorf("stale on_type edge survived: To=%q, want schema.graphql::Author", onTypeEdges[0].To)
	}
}
