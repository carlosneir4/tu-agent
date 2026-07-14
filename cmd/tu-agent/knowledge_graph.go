package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/graph"
)

// buildKnowledgeNodes turns generated skills and the CLAUDE.md conventions block
// into knowledge-layer nodes and documents edges. Pure over its inputs except
// for reading CLAUDE.md to locate the conventions block's line range.
func buildKnowledgeNodes(skills []codegen.Skill, root string) ([]graph.Node, []graph.Edge) {
	var nodes []graph.Node
	var edges []graph.Edge
	for _, sk := range skills {
		id := "skill::" + sk.Name
		// Convention: node ID is "skill::<name>"; a store-sourced card has no file,
		// so its Path is the virtual pointer "concept::<name>" — deliberately NOT a
		// real path and NOT a graph node ID (nothing else registers concept:: IDs).
		path := "concept::" + sk.Name
		if sk.Dir != "" { // a real on-disk skill (e.g. architecture)
			rel, err := filepath.Rel(root, filepath.Join(sk.Dir, "SKILL.md"))
			if err != nil {
				rel = filepath.Join(sk.Dir, "SKILL.md")
			}
			path = rel
		}
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindSkill, Name: sk.Name, Path: path, Line: 1})
		for _, kf := range codegen.ParseKeyFiles(sk.Body) {
			edges = append(edges, graph.Edge{From: id, To: kf, Kind: graph.EdgeDocuments, Confidence: graph.ConfHigh})
		}
	}
	if line, end, ok := markerLineRange(filepath.Join(root, "CLAUDE.md"), projectContextOpen, projectContextClose); ok {
		nodes = append(nodes, graph.Node{
			ID: "convention::project", Kind: graph.KindConvention, Name: "conventions",
			Path: "CLAUDE.md", Line: line, EndLine: end,
		})
	}
	return nodes, edges
}

// markerLineRange returns the 1-based line numbers of the open and close marker
// lines in the file, and whether both were found.
func markerLineRange(path, open, close string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var openLine, closeLine int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if openLine == 0 && strings.Contains(line, open) {
			openLine = n
		}
		if strings.Contains(line, close) {
			closeLine = n
		}
	}
	// A scan error (e.g. a line longer than the buffer) means we read only part
	// of the file; don't trust a possibly-stale range — report not-found.
	if sc.Err() != nil {
		return 0, 0, false
	}
	if openLine == 0 || closeLine <= openLine {
		return 0, 0, false
	}
	return openLine, closeLine, true
}
