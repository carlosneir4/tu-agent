package testgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/query"
)

// contextFixture writes a tiny repo and builds a matching in-memory graph:
// caller run() in cmd/main.go calls Store.Save in internal/store/store.go,
// which calls DB.Put; a skill documents the store file.
func contextFixture(t *testing.T) (*query.Graph, string, Target) {
	t.Helper()
	root := t.TempDir()
	store := `package store

type Store struct{ db DB }

// Save persists v.
func (s *Store) Save(v string) error {
	return s.db.Put(v)
}
`
	main := `package main

func run(st *store.Store, input string) error {
	if err := st.Save(input); err != nil {
		return err
	}
	return nil
}
`
	files := map[string]string{
		"internal/store/store.go":      store,
		"cmd/main.go":                  main,
		".claude/skills/core/SKILL.md": "Store persists via DB.Put.",
	}
	for p, content := range files {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nodes := []graph.Node{
		{ID: "internal/store/store.go::Store.Save", Kind: graph.KindFunction, Name: "Store.Save",
			Path: "internal/store/store.go", Line: 6, EndLine: 8, Language: "go",
			Params: "(v string)", ReturnType: "error", Exported: true},
		{ID: "cmd/main.go::run", Kind: graph.KindFunction, Name: "run",
			Path: "cmd/main.go", Line: 3, EndLine: 9, Language: "go"},
		{ID: "internal/store/db.go::DB.Put", Kind: graph.KindFunction, Name: "DB.Put",
			Path: "internal/store/db.go", Line: 1, EndLine: 1, Language: "go",
			Params: "(v string)", ReturnType: "error"},
		{ID: "skill::core", Kind: graph.KindSkill, Name: "core", Path: ".claude/skills/core/SKILL.md"},
	}
	edges := []graph.Edge{
		{From: "cmd/main.go::run", To: "internal/store/store.go::Store.Save", Kind: graph.EdgeCalls},
		{From: "internal/store/store.go::Store.Save", To: "internal/store/db.go::DB.Put", Kind: graph.EdgeCalls},
		{From: "skill::core", To: "internal/store/store.go", Kind: graph.EdgeDocuments},
	}
	g := query.NewGraph(nodes, edges)
	tgt := TargetFromNode(nodes[0])
	return g, root, tgt
}

func TestBuildContext(t *testing.T) {
	g, root, tgt := contextFixture(t)
	gc, err := BuildContext(g, root, tgt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if gc.PackageClause != "package store" {
		t.Errorf("PackageClause = %q", gc.PackageClause)
	}
	if !strings.Contains(gc.Body, "func (s *Store) Save") || !strings.Contains(gc.Body, "s.db.Put(v)") {
		t.Errorf("Body wrong:\n%s", gc.Body)
	}
	if len(gc.CallSites) != 1 || !strings.Contains(gc.CallSites[0].Snippet, "st.Save(input)") {
		t.Fatalf("CallSites = %+v", gc.CallSites)
	}
	if !strings.Contains(gc.CallSites[0].Caller, "cmd/main.go:4") {
		t.Errorf("call site pointer = %q, want cmd/main.go:4", gc.CallSites[0].Caller)
	}
	if len(gc.Callees) != 1 || !strings.Contains(gc.Callees[0], "DB.Put") {
		t.Errorf("Callees = %+v", gc.Callees)
	}
	if gc.SkillExcerpt != "Store persists via DB.Put." {
		t.Errorf("SkillExcerpt = %q", gc.SkillExcerpt)
	}
	if gc.BlastRadius < 1 {
		t.Errorf("BlastRadius = %d, want >= 1", gc.BlastRadius)
	}
}

func TestBuildContextBudget(t *testing.T) {
	g, root, tgt := contextFixture(t)
	full, err := BuildContext(g, root, tgt, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Budget exactly the body + package clause: call sites/callees/skill
	// must be dropped, the body never truncated.
	tight, err := BuildContext(g, root, tgt, len(full.Body)+len(full.PackageClause))
	if err != nil {
		t.Fatal(err)
	}
	if tight.Body != full.Body {
		t.Error("body must never be truncated")
	}
	if len(tight.CallSites) != 0 || len(tight.Callees) != 0 || tight.SkillExcerpt != "" {
		t.Errorf("over-budget extras kept: %+v", tight)
	}
}

func TestBuildContextUnlocatableCallSite(t *testing.T) {
	g, root, tgt := contextFixture(t)
	// Overwrite the caller file so the symbol no longer appears in its span.
	if err := os.WriteFile(filepath.Join(root, "cmd/main.go"),
		[]byte("package main\n\nfunc run() {}\n\n\n\n\n\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gc, err := BuildContext(g, root, tgt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gc.CallSites) != 0 {
		t.Errorf("unlocatable call site must be omitted, got %+v", gc.CallSites)
	}
}
