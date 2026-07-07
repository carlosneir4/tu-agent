package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/store"
)

const conceptMaxLandmarks = 10

// buildConceptCardsFromUnits assembles concept cards: root-based discovery or
// domain-map fallback, then landmarks/traits from node-level graph data.
// edges/weighted are the import graph and file-coupling weights from
// loadSourceUnits; they must reach buildDomains so `--cluster leiden` can
// actually run topology clustering instead of always falling back to the
// package-path heuristic (BuildDomainMapClustered falls back whenever
// weighted is empty).
func buildConceptCardsFromUnits(units []codegen.SourceUnit, edges []codegen.Edge, weighted []codegen.WeightedEdge, nodes []graph.Node, nodeEdges []graph.Edge, conceptRoots []string, opts codegen.DomainMapOptions, cluster string) ([]codegen.ConceptCard, error) {
	concepts := codegen.DiscoverConcepts(units, conceptRoots)
	if concepts == nil {
		domains, err := buildDomains(units, edges, weighted, opts, cluster)
		if err != nil {
			return nil, fmt.Errorf("concepts: domain fallback: %w", err)
		}
		concepts = codegen.ConceptsFromDomains(domains)
	}
	return codegen.BuildConceptCards(concepts, nodes, nodeEdges, conceptMaxLandmarks), nil
}

// runConcepts builds the graph, computes cards, and prints them (text or JSON).
// It never writes skill files — that is learn's job.
func runConcepts(subpath string, conceptRoots []string, cluster string, opts codegen.DomainMapOptions, jsonOut bool) (string, error) {
	if err := runGraphBuild(subpath); err != nil {
		return "", fmt.Errorf("concepts: building graph: %w", err)
	}
	s, err := openGraphStore()
	if err != nil {
		return "", fmt.Errorf("concepts: opening store: %w", err)
	}
	units, edges, weighted, err := loadSourceUnits(s)
	if err != nil {
		s.Close()
		return "", err
	}
	nodes, err := s.AllNodes()
	if err != nil {
		s.Close()
		return "", fmt.Errorf("concepts: %w", err)
	}
	nodeEdges, err := s.AllEdges()
	s.Close()
	if err != nil {
		return "", fmt.Errorf("concepts: %w", err)
	}
	cards, err := buildConceptCardsFromUnits(units, edges, weighted, nodes, nodeEdges, conceptRoots, opts, cluster)
	if err != nil {
		return "", err
	}
	if jsonOut {
		data, err := json.MarshalIndent(cards, "", "  ")
		if err != nil {
			return "", fmt.Errorf("concepts: marshalling: %w", err)
		}
		return string(data) + "\n", nil
	}
	var sb strings.Builder
	for _, c := range cards {
		sb.WriteString(codegen.RenderConceptCard(c))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// persistConceptCards renders each card and stores it in the graph DB. It opens
// the store itself (the learn flow has already closed its handle by this point).
func persistConceptCards(cards []codegen.ConceptCard) error {
	st, err := openGraphStore()
	if err != nil {
		return err
	}
	defer st.Close()
	return persistConceptCardsTo(st, cards)
}

// persistConceptCardsTo persists cards into st. Splitting it out lets tests
// inject a temp store. Preserves a prior generated description when this run has
// none (e.g. --skip-llm), mirroring the old on-disk PreservedDefinition behavior.
func persistConceptCardsTo(st *store.Store, cards []codegen.ConceptCard) error {
	prev := map[string]string{}
	existing, err := st.ListConcepts()
	if err != nil {
		return fmt.Errorf("concepts: reading existing: %w", err)
	}
	for _, r := range existing {
		prev[r.Name] = r.Description
	}
	rows := make([]store.ConceptRow, 0, len(cards))
	for _, c := range cards {
		c.Definition = codegen.PreservedDefinition(c.Definition, prev[c.Name])
		content := codegen.RenderConceptCard(c)
		desc := c.Definition
		if sk, perr := codegen.ParseSkillContent(content); perr == nil {
			desc = sk.Description // authoritative: exactly what the frontmatter holds
		} else {
			slog.Debug("concepts: description parse fallback", "name", c.Name, "err", perr)
		}
		rows = append(rows, store.ConceptRow{Name: c.Name, Description: desc, Content: content})
	}
	if err := st.ReplaceConcepts(rows); err != nil {
		return fmt.Errorf("concepts: persisting: %w", err)
	}
	return nil
}

var (
	conceptsRoot    []string
	conceptsJSON    bool
	conceptsCluster string
)

var conceptsCmd = &cobra.Command{
	Use:   "concepts [path]",
	Short: "Print the concept index cards (deterministic; no model calls)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := ""
		if len(args) == 1 {
			sub = args[0]
		}
		roots := resolveConceptRoots(conceptsRoot, repoRoot())
		opts := codegen.DomainMapOptions{Depth: 1, MinFiles: 5, MaxFiles: cfg.Learn.MaxFilesPerDomain, MinStandaloneFiles: cfg.Learn.MinStandaloneFiles}
		out, err := runConcepts(sub, roots, conceptsCluster, opts, conceptsJSON)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

// setConceptDefinition rewrites a stored concept card's one-line description to
// def and persists it (description column + frontmatter in the rendered content
// stay in sync). Shared by the `concepts set-definition` CLI subcommand and the
// set_concept_definition MCP tool. Deterministic — no model call. This is the
// write-back path the plugin's generative learn step uses now that cards live in
// the store instead of on-disk SKILL.md files.
func setConceptDefinition(name, def string) error {
	st, err := openGraphStore()
	if err != nil {
		return err
	}
	defer st.Close()
	row, ok, err := st.GetConcept(name)
	if err != nil {
		return fmt.Errorf("set-definition: %w", err)
	}
	if !ok {
		return fmt.Errorf("set-definition: no concept %q", name)
	}
	content, err := codegen.SetCardDescription(row.Content, def)
	if err != nil {
		return fmt.Errorf("set-definition: %w", err)
	}
	return st.UpsertConcept(store.ConceptRow{Name: name, Description: def, Content: content})
}

var conceptsSetDefCmd = &cobra.Command{
	Use:   "set-definition <name> <definition>",
	Short: "Set a concept card's one-line definition in the graph store (no model call)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := setConceptDefinition(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("set definition for %q\n", args[0])
		return nil
	},
}

func init() {
	conceptsCmd.Flags().StringArrayVar(&conceptsRoot, "concept-root", nil, "package(s) whose direct subpackages define concepts; repeatable. Default: auto-detect from package.json workspaces")
	conceptsCmd.Flags().BoolVar(&conceptsJSON, "json", false, "emit cards as JSON")
	conceptsCmd.Flags().StringVar(&conceptsCluster, "cluster", "leiden", "fallback clustering when no concept root is set")
	conceptsCmd.AddCommand(conceptsSetDefCmd)
	rootCmd.AddCommand(conceptsCmd)
}
