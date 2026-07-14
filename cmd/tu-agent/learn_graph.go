package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// loadSourceUnits reads the graph store into language-neutral source units,
// file-level import edges (consumed by the merge guardrail and structural
// context), and weighted file-level coupling edges — imports, calls,
// extends, and implements aggregated per file pair — for topology clustering.
func loadSourceUnits(s *store.Store) ([]codegen.SourceUnit, []codegen.Edge, []codegen.WeightedEdge, error) {
	files, err := s.Files()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loadSourceUnits: %w", err)
	}
	units := make([]codegen.SourceUnit, 0, len(files))
	for _, f := range files {
		if f.Status != "ok" {
			continue
		}
		class := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
		fqn := class
		if f.Package != "" {
			fqn = f.Package + "." + class
		}
		units = append(units, codegen.SourceUnit{
			Path: f.Path, Package: f.Package, FQN: fqn, Size: f.Size, Language: f.Language,
		})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Path < units[j].Path })

	nodes, err := s.AllNodes()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loadSourceUnits: %w", err)
	}
	nodePath := make(map[string]string, len(nodes))
	for _, nd := range nodes {
		if nd.Path != "" {
			nodePath[nd.ID] = nd.Path
		}
	}

	allEdges, err := s.AllEdges()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loadSourceUnits: %w", err)
	}
	var fe []codegen.Edge
	pair := map[[2]string]int{}
	for _, e := range allEdges {
		var fp, tp string
		switch e.Kind {
		case graph.EdgeImports:
			fp, tp = e.From, e.To // imports edges are already file -> file
			fe = append(fe, codegen.Edge{From: e.From, To: e.To})
		case graph.EdgeCalls, graph.EdgeExtends, graph.EdgeImplements:
			fp, tp = nodePath[e.From], nodePath[e.To] // external:: stubs have no path and drop out
		default:
			continue
		}
		if fp == "" || tp == "" || fp == tp {
			continue
		}
		// Canonicalize direction so (A,B) and (B,A) accumulate to the same pair.
		if fp > tp {
			fp, tp = tp, fp
		}
		pair[[2]string{fp, tp}]++
	}
	keys := make([][2]string, 0, len(pair))
	for k := range pair {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	weighted := make([]codegen.WeightedEdge, 0, len(keys))
	for _, k := range keys {
		weighted = append(weighted, codegen.WeightedEdge{From: k[0], To: k[1], Weight: pair[k]})
	}
	return units, fe, weighted, nil
}

// registerKnowledge indexes concepts from the store (+ the architecture skill
// from disk) into the graph's knowledge layer. Idempotent; safe to re-run.
func registerKnowledge(root string) error {
	s, err := openGraphStore()
	if err != nil {
		return err
	}
	defer s.Close()

	concepts, err := s.ListConcepts()
	if err != nil {
		return fmt.Errorf("registerKnowledge: %w", err)
	}
	skills := make([]codegen.Skill, 0, len(concepts)+1)
	for _, c := range concepts {
		sk, perr := codegen.ParseSkillContent(c.Content)
		if perr != nil {
			// Best-effort indexing: a malformed card must not abort learn
			// finalization. (loadConceptSkills, feeding synthesis/status, is
			// stricter and fails hard on the same condition — see its doc.)
			slog.Warn("registerKnowledge: skipping malformed concept", "name", c.Name, "err", perr)
			continue
		}
		if sk.Name == "" { // no name: in frontmatter → would yield a garbage skill:: node
			slog.Warn("registerKnowledge: skipping unnamed concept", "store_name", c.Name)
			continue
		}
		skills = append(skills, sk) // Dir == "" → virtual concept:: path
	}
	// F7-A: the architecture overview no longer lives as a .claude/skills file;
	// it is stored in graph.db metadata and read via get_architecture, so it is
	// not indexed as a knowledge-graph skill node.

	nodes, edges := buildKnowledgeNodes(skills, root)
	if err := s.ReplaceKnowledge(nodes, edges); err != nil {
		return fmt.Errorf("registerKnowledge: %w", err)
	}
	return nil
}

// buildDomains selects the domain-map clustering strategy. "leiden" (the
// default) clusters by graph topology with heuristic fallback on small
// repos; "heuristic" forces the package-path strategy (escape hatch and
// comparison baseline for the M2.1 success criterion).
func buildDomains(units []codegen.SourceUnit, edges []codegen.Edge, weighted []codegen.WeightedEdge, opts codegen.DomainMapOptions, cluster string) ([]codegen.Domain, error) {
	switch cluster {
	case "", "leiden":
		return codegen.BuildDomainMapClustered(units, edges, weighted, opts), nil
	case "heuristic":
		return codegen.BuildDomainMap(units, edges, opts), nil
	default:
		return nil, fmt.Errorf("buildDomains: unknown cluster strategy %q (use leiden or heuristic)", cluster)
	}
}
