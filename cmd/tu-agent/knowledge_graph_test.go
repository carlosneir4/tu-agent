package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/graph"
)

// markerLineRange must not trust a partial scan: a file whose content exceeds
// the scanner's line buffer triggers a scan error, and the function must report
// not-found rather than return a possibly-stale range.
func TestMarkerLineRange_ScanErrorReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	var b strings.Builder
	b.WriteString(projectContextOpen + "\n")
	b.WriteString("body\n")
	b.WriteString(projectContextClose + "\n")
	// A line longer than the 1 MiB scanner buffer → bufio.ErrTooLong on Scan.
	b.WriteString(strings.Repeat("x", 2*1024*1024) + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := markerLineRange(path, projectContextOpen, projectContextClose); ok {
		t.Error("markerLineRange must return ok=false when the scan errors")
	}
}

// Sanity: a well-formed marker block on a normal file still resolves.
func TestMarkerLineRange_FindsBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	content := "intro\n" + projectContextOpen + "\nbody\n" + projectContextClose + "\nrest\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	open, end, ok := markerLineRange(path, projectContextOpen, projectContextClose)
	if !ok || open != 2 || end != 4 {
		t.Errorf("markerLineRange = (%d, %d, %v), want (2, 4, true)", open, end, ok)
	}
}

func TestBuildKnowledgeNodes(t *testing.T) {
	root := t.TempDir()
	skills := []codegen.Skill{{
		Name:        "billing",
		Description: "Billing domain",
		Dir:         filepath.Join(root, ".claude", "skills", "billing"),
		Body:        "## Key Files\n- src/billing/InvoiceService.java: core\n- src/billing/Ledger.java\n",
	}}
	claude := "# Title\n\n" + projectContextOpen + "\nConventions here\n" + projectContextClose + "\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}

	nodes, edges := buildKnowledgeNodes(skills, root)

	var skillNode, convNode *graph.Node
	for i := range nodes {
		switch nodes[i].Kind {
		case graph.KindSkill:
			skillNode = &nodes[i]
		case graph.KindConvention:
			convNode = &nodes[i]
		}
	}
	if skillNode == nil || skillNode.ID != "skill::billing" {
		t.Fatalf("skill node = %+v", skillNode)
	}
	if convNode == nil || convNode.Path != "CLAUDE.md" || convNode.Line == 0 || convNode.EndLine <= convNode.Line {
		t.Fatalf("convention node line range wrong: %+v", convNode)
	}
	var targets []string
	for _, e := range edges {
		if e.Kind == graph.EdgeDocuments && e.From == "skill::billing" {
			targets = append(targets, e.To)
		}
	}
	if len(targets) != 2 {
		t.Errorf("documents edges = %v, want 2 key files", targets)
	}
}

func TestBuildKnowledgeNodesNoConventionsBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Title only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nodes, _ := buildKnowledgeNodes(nil, root)
	for _, n := range nodes {
		if n.Kind == graph.KindConvention {
			t.Errorf("no conventions block present, but got a convention node: %+v", n)
		}
	}
}

func TestBuildKnowledgeNodesFromConcepts(t *testing.T) {
	skills := []codegen.Skill{
		{Name: "widgets", Description: "d", Body: "## key files\n- core/Widget.java: the type\n"}, // Dir empty → store-sourced
	}
	nodes, edges := buildKnowledgeNodes(skills, t.TempDir())
	var found bool
	for _, n := range nodes {
		if n.ID == "skill::widgets" {
			found = true
			if n.Path != "concept::widgets" {
				t.Errorf("store concept node Path = %q, want concept::widgets", n.Path)
			}
		}
	}
	if !found {
		t.Fatalf("no skill::widgets node; got %+v", nodes)
	}
	var hasDoc bool
	for _, e := range edges {
		if e.From == "skill::widgets" && e.To == "core/Widget.java" && e.Kind == graph.EdgeDocuments {
			hasDoc = true
		}
	}
	if !hasDoc {
		t.Errorf("missing documents edge to core/Widget.java; got %+v", edges)
	}
}
