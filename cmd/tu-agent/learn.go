package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/spf13/cobra"
)

// learnOpts controls the learn pipeline.
type learnOpts struct {
	Subpath            string
	Patterns           string
	Depth              int
	MinFiles           int
	MaxFiles           int
	MinStandaloneFiles int
	Provider           string
	SkipLLM            bool
	Cluster            string
	ConceptRoots       []string
}

// runLearn executes the knowledge pipeline in four phases with an explicit
// LLM/static boundary: graph build → concept index (deterministic) →
// definitions + architecture (LLM) → static register. --skip-llm skips
// phase 3 only: cards keep their deterministic descriptions.
func runLearn(ctx context.Context, opts learnOpts) error {
	fmt.Println("[1/4] graph: building dependency graph")
	if err := runGraphBuild(opts.Subpath); err != nil {
		return fmt.Errorf("building graph: %w", err)
	}

	fmt.Println("\n[2/4] concepts: computing index cards")
	s, err := openGraphStore()
	if err != nil {
		return err
	}
	units, gEdges, weighted, err := loadSourceUnits(s)
	if err != nil {
		s.Close()
		return err
	}
	nodes, err := s.AllNodes()
	if err != nil {
		s.Close()
		return fmt.Errorf("learn: %w", err)
	}
	nodeEdges, err := s.AllEdges()
	s.Close()
	if err != nil {
		return fmt.Errorf("learn: %w", err)
	}
	if len(units) == 0 {
		return fmt.Errorf("no source files found under %q", opts.Subpath)
	}
	roots := resolveConceptRoots(opts.ConceptRoots, repoRoot())
	mapOpts := codegen.DomainMapOptions{
		Depth: opts.Depth, MinFiles: opts.MinFiles, MaxFiles: opts.MaxFiles,
		MinStandaloneFiles: opts.MinStandaloneFiles,
	}
	cards, err := buildConceptCardsFromUnits(units, gEdges, weighted, nodes, nodeEdges, roots, mapOpts, opts.Cluster)
	if err != nil {
		return err
	}
	fmt.Printf("Mapped %d file(s) into %d concept(s)\n", len(units), len(cards))

	if opts.SkipLLM {
		fmt.Println("--skip-llm: cards keep deterministic descriptions; skipping architecture synthesis")
	} else {
		fmt.Println("\n[3/4] definitions + architecture (one model call each)")
		prov, perr := selectProvider(cfg, "init", opts.Provider)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "warning: definitions provider: %v (keeping deterministic descriptions)\n", perr)
		} else {
			tel, terr := telemetry.NewLogger(telemetryPath(repoRoot()))
			if terr != nil {
				fmt.Fprintf(os.Stderr, "warning: telemetry init: %v\n", terr)
			}
			defs, derr := codegen.GenerateConceptDefinitions(ctx, cards, prov, tel)
			if derr != nil {
				fmt.Fprintf(os.Stderr, "warning: concept definitions: %v (keeping deterministic descriptions)\n", derr)
			} else {
				codegen.ApplyDefinitions(cards, defs)
			}
		}
	}

	if err := persistConceptCards(cards); err != nil {
		return err
	}
	if !opts.SkipLLM {
		runSynthesizeAndEnrich(ctx, opts)
	}

	fmt.Println("\n[4/4] register: knowledge block")
	registerStatic()
	fmt.Println(learnCompletionReminder())
	return nil
}

// learnCompletionReminder is printed after a learn run. The knowledge protocol
// lives in CLAUDE.md, which an agent loads at session start — so a session that
// was already open when learn ran won't have it. Tell the user to start fresh.
func learnCompletionReminder() string {
	return "\nDone. Start a NEW session (or /clear) so the updated CLAUDE.md protocol " +
		"takes effect —\notherwise the current session won't use the graph/skills for structural questions."
}

// runSynthesizeAndEnrich is phase 3 (LLM): it writes the architecture skill and
// the CLAUDE.md project-context block. Failures warn but never abort — the
// concept cards on disk are the primary artifact.
func runSynthesizeAndEnrich(ctx context.Context, opts learnOpts) {
	task := "synthesize"
	if _, ok := cfg.Routing.Tasks["synthesize"]; !ok {
		if _, ok := cfg.Routing.Tasks["consolidate"]; ok {
			task = "consolidate"
		} else if _, ok := cfg.Routing.Tasks["init"]; ok {
			task = "init"
		}
	}
	prov, err := selectProvider(cfg, task, opts.Provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: synthesize provider: %v\n", err)
		return
	}
	ctxSize := effectiveContextSize(
		cfg.Providers[resolveProviderName(cfg, task, opts.Provider)].ContextSize,
		prov,
	)
	mergedSynthesizeAndEnrich(ctx, repoRoot(), prov, ctxSize)
}

// registerStatic is phase 4 (no LLM): prune empty skill dirs, write the knowledge
// block and register the knowledge-layer graph.
func registerStatic() {
	root := repoRoot()
	if pruned, err := codegen.PruneEmptySkillDirs(generatedSkillsDir(root)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: pruning empty skill dirs: %v\n", err)
	} else if len(pruned) > 0 {
		fmt.Printf("Pruned %d empty skill dir(s): %v\n", len(pruned), pruned)
	}
	if err := writeKnowledgeBlock(filepath.Join(root, "CLAUDE.md")); err != nil {
		fmt.Fprintf(os.Stderr, "warning: knowledge block: %v\n", err)
	}
	if err := registerKnowledge(root); err != nil {
		fmt.Fprintf(os.Stderr, "warning: registering knowledge graph: %v\n", err)
	}
}

var (
	learnDepth         int
	learnMinFiles      int
	learnMaxFiles      int
	learnMinStandalone int
	learnProvider      string
	learnSkipLLM       bool
	learnCluster       string
	learnConceptRoots  []string
)

var learnCmd = &cobra.Command{
	GroupID: "graph",
	Use:     "learn [path]",
	Short:   "Build the concept index and register project knowledge in the graph store",
	Long: `Builds the dependency graph, computes one concept card per package cluster,
optionally fills definitions with a single model call, stores the cards in the
graph store (graph.db, queryable via get_concept), and registers the
knowledge-pointer block in CLAUDE.md.

Cards are ≤1 KB each; the whole pipeline is re-runnable and idempotent.
Use --skip-llm to refresh the index without any model calls.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		maxFiles := learnMaxFiles
		if !cmd.Flags().Changed("max-files-per-domain") && cfg.Learn.MaxFilesPerDomain > 0 {
			maxFiles = cfg.Learn.MaxFilesPerDomain
		}
		minStandalone := learnMinStandalone
		if !cmd.Flags().Changed("min-standalone-files") && cfg.Learn.MinStandaloneFiles > 0 {
			minStandalone = cfg.Learn.MinStandaloneFiles
		}
		o := learnOpts{
			Patterns:           initPatterns,
			Depth:              learnDepth,
			MinFiles:           learnMinFiles,
			MaxFiles:           maxFiles,
			MinStandaloneFiles: minStandalone,
			Provider:           learnProvider,
			SkipLLM:            learnSkipLLM,
			Cluster:            learnCluster,
			ConceptRoots:       learnConceptRoots,
		}
		if len(args) == 1 {
			o.Subpath = args[0]
		}
		return runLearn(cmd.Context(), o)
	},
}

func init() {
	learnCmd.Flags().StringVar(&initPatterns, "patterns", defaultInitPatterns, "comma-separated file extensions to scan")
	learnCmd.Flags().IntVar(&learnDepth, "depth", 1, "package segments below the common root that define a domain")
	learnCmd.Flags().IntVar(&learnMinFiles, "min-files", 5, "domains smaller than this merge into the most-coupled sibling")
	learnCmd.Flags().IntVar(&learnMaxFiles, "max-files-per-domain", 0,
		"optional extra cap: split domains with more files than this (0 = config default, 40)")
	learnCmd.Flags().IntVar(&learnMinStandalone, "min-standalone-files", 0,
		"packages with this many non-test files never merge (0 = config default, 4)")
	learnCmd.Flags().StringVar(&learnProvider, "provider", "", "provider override (claude|local) for definitions and architecture synthesis")
	learnCmd.Flags().BoolVar(&learnSkipLLM, "skip-llm", false, "write cards with deterministic descriptions only (no model calls)")
	learnCmd.Flags().StringVar(&learnCluster, "cluster", "leiden", "fallback clustering when no concept root is set")
	learnCmd.Flags().StringArrayVar(&learnConceptRoots, "concept-root", nil, "package(s) whose direct subpackages define concepts; repeatable. Default: auto-detect from package.json workspaces")
}
