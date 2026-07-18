package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/spf13/cobra"
)

// loadConceptSkills reads concept cards from the graph store and parses each
// into a codegen.Skill (Dir == "" → store-sourced). Used by synthesis + status,
// which are correctness-sensitive: a malformed card is a hard error here (fail
// loud) — deliberately stricter than registerKnowledge, which is best-effort
// indexing during learn finalization and skips a bad card with a warning.
func loadConceptSkills() ([]codegen.Skill, error) {
	st, err := openGraphStore()
	if err != nil {
		return nil, err
	}
	rows, err := st.ListConcepts()
	st.Close()
	if err != nil {
		return nil, fmt.Errorf("loadConceptSkills: %w", err)
	}
	out := make([]codegen.Skill, 0, len(rows))
	for _, r := range rows {
		sk, perr := codegen.ParseSkillContent(r.Content)
		if perr != nil {
			return nil, fmt.Errorf("loadConceptSkills: parsing %s: %w", r.Name, perr)
		}
		sk.Files = r.Files // member files from the store's concept->files link
		out = append(out, sk)
	}
	return out, nil
}

// loadConceptSkillDocs reads concept cards from the store as raw SkillDocs
// (name + full content) for the architecture-synthesis prompt.
func loadConceptSkillDocs() ([]codegen.SkillDoc, error) {
	st, err := openGraphStore()
	if err != nil {
		return nil, err
	}
	rows, err := st.ListConcepts()
	st.Close()
	if err != nil {
		return nil, fmt.Errorf("loadConceptSkillDocs: %w", err)
	}
	out := make([]codegen.SkillDoc, 0, len(rows))
	for _, r := range rows {
		out = append(out, codegen.SkillDoc{Name: r.Name, Content: r.Content})
	}
	return out, nil
}

// prepareSynthesisInputs runs the deterministic part of synthesis: scan Java,
// persist the file-level graph, load skills, and aggregate domain edges. It is
// the testable seam (no model call) and is read-only w.r.t. disk state. When
// buildGraph is true the graph is rebuilt from source; pass false when the
// caller has already built it.
func prepareSynthesisInputs(root, subpath string, buildGraph bool) (string, []codegen.DomainFact, []codegen.Edge, error) {
	if buildGraph {
		if err := runGraphBuild(subpath); err != nil {
			return "", nil, nil, fmt.Errorf("building graph: %w", err)
		}
	}
	s, err := openGraphStore()
	if err != nil {
		return "", nil, nil, err
	}
	_, edges, _, err := loadSourceUnits(s)
	if cerr := s.Close(); cerr != nil {
		slog.Warn("learn: closing graph store", "err", cerr)
	}
	if err != nil {
		return "", nil, nil, err
	}

	// Second store open (loadSourceUnits above used and closed the first);
	// loadConceptSkills is self-contained. SQLite WAL tolerates this.
	skills, err := loadConceptSkills()
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading concepts: %w", err)
	}
	if len(skills) == 0 {
		return "", nil, nil, fmt.Errorf("no concepts found — run 'tu-agent learn <path>' first")
	}
	var domains []codegen.DomainFact
	for _, s := range skills {
		if s.Name == "architecture" {
			continue
		}
		domains = append(domains, codegen.DomainFact{
			Name: s.Name, Description: s.Description, KeyFiles: s.Files,
		})
	}
	if len(domains) == 0 {
		return "", nil, nil, fmt.Errorf("no domain skills found — run 'tu-agent learn <path>' first")
	}

	fileToDomain := codegen.BuildFileToDomain(skills)
	domainEdges := codegen.AggregateToDomains(edges, fileToDomain)

	abs, _ := filepath.Abs(root)
	return filepath.Base(abs), domains, domainEdges, nil
}

func runSynthesize(ctx context.Context, subpath, providerOverride string) error {
	root := "."
	project, domains, domainEdges, err := prepareSynthesisInputs(root, subpath, true)
	if err != nil {
		return err
	}
	task := "synthesize"
	if _, ok := cfg.Routing.Tasks["synthesize"]; !ok {
		if _, ok := cfg.Routing.Tasks["consolidate"]; ok {
			task = "consolidate"
		} else if _, ok := cfg.Routing.Tasks["init"]; ok {
			task = "init"
		}
	}
	prov, err := selectProvider(cfg, task, providerOverride)
	if err != nil {
		return err
	}
	tel, err := telemetry.NewLogger(telemetryPath(root))
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}

	contextSize := effectiveContextSize(
		cfg.Providers[resolveProviderName(cfg, task, providerOverride)].ContextSize,
		prov,
	)
	content, err := codegen.GenerateArchitecture(ctx, project, domains, domainEdges, prov, tel, contextSize)
	if err != nil {
		return err
	}
	// Persist the overview to the graph store (F7-A: the narrative lives in
	// graph.db, read via get_architecture / `tu-agent graph architecture`).
	wrote, err := persistArchitecture(content)
	if err != nil {
		return err
	}
	if !wrote {
		// The model produced no usable overview (empty after stripping
		// frontmatter), so the store still holds the old/absent overview.
		fmt.Fprintln(os.Stderr, "warning: synthesis produced an empty architecture overview — nothing stored")
		return nil
	}
	fmt.Printf("Stored architecture overview (%d domains, %d domain edges)\n", len(domains), len(domainEdges))
	return nil
}

// mergedSynthesizeAndEnrich runs phase 4 via the single merged model call,
// writing the architecture skill and upserting the CLAUDE.md project-context
// block. On an unparseable merged response it falls back to the two separate
// generators. Failures of the fallback warn but do not abort.
func mergedSynthesizeAndEnrich(ctx context.Context, root string, prov provider.Provider, contextSize int) {
	project, domains, domainEdges, err := prepareSynthesisInputs(root, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: synthesize inputs: %v\n", err)
		return
	}
	skills, err := loadConceptSkillDocs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: reading concepts: %v\n", err)
		return
	}
	tel, telErr := telemetry.NewLogger(telemetryPath(root))
	if telErr != nil {
		slog.Warn("learn: telemetry init failed, proceeding without logging", "err", telErr)
	}

	arch, ctxBlock, err := codegen.GenerateArchitectureAndContext(ctx, project, domains, domainEdges, skills, prov, tel, contextSize)
	if errors.Is(err, codegen.ErrMergedParseFailed) {
		fmt.Fprintln(os.Stderr, "note: merged synthesis unparseable; falling back to two calls")
		if a, aerr := codegen.GenerateArchitecture(ctx, project, domains, domainEdges, prov, tel, contextSize); aerr == nil {
			arch = a
		} else {
			fmt.Fprintf(os.Stderr, "warning: synthesize: %v\n", aerr)
		}
		if c, cerr := codegen.GenerateProjectContext(ctx, project, skills, prov, tel, contextSize); cerr == nil {
			ctxBlock = c
		} else {
			fmt.Fprintf(os.Stderr, "warning: CLAUDE.md enrichment: %v\n", cerr)
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "warning: merged synthesis: %v\n", err)
		return
	}

	if arch != "" {
		if wrote, wErr := persistArchitecture(arch); wErr != nil {
			fmt.Fprintf(os.Stderr, "warning: storing architecture overview: %v\n", wErr)
		} else if !wrote {
			// arch was non-empty but stripped to nothing (frontmatter only), so
			// the store keeps its old/absent overview (mirrors runSynthesize).
			fmt.Fprintln(os.Stderr, "warning: merged synthesis produced an empty architecture overview — nothing stored")
		}
	}
	if ctxBlock != "" {
		body := projectContextOpen + "\n" + ctxBlock + "\n" + projectContextClose
		if uErr := upsertMarkedBlock(filepath.Join(root, "CLAUDE.md"), projectContextOpen, projectContextClose, body); uErr != nil {
			fmt.Fprintf(os.Stderr, "warning: project-context block: %v\n", uErr)
		}
	}
}

var synthesizeProvider string

var synthesizeCmd = &cobra.Command{
	Use:   "synthesize [path]",
	Short: "Synthesize an architecture overview skill from the concept index",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := ""
		if len(args) == 1 {
			sub = args[0]
		}
		return runSynthesize(cmd.Context(), sub, synthesizeProvider)
	},
}

func runStatus(root string) error {
	return runStatusTo(os.Stdout, root)
}

// conceptState is the freshness of one concept's generated skill.
type conceptState struct {
	Name   string
	Status string // "up-to-date" | "stale" | "new"
}

// conceptStates reports each concept's freshness, ordered by name. The
// "architecture" concept is skipped (derived, not a domain).
//
// Member files come from the store's concept -> files link, never from the
// concept card's body: bodies carry no "## Key Files" section since concepts
// moved into graph.db, which is exactly what made the retired
// skill-fingerprints.json hash the empty string for every concept and report
// "up-to-date" forever.
func conceptStates(root string) ([]conceptState, error) {
	st, err := openGraphStore()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	concepts, err := st.ListConcepts()
	if err != nil {
		return nil, fmt.Errorf("loading concepts: %w", err)
	}
	if len(concepts) == 0 {
		return nil, fmt.Errorf("no concepts found — run 'tu-agent learn <path>' first")
	}
	// The sha256 the graph recorded per file when it was last built — the "as
	// learned" baseline that live content is compared against.
	recorded, err := st.Files()
	if err != nil {
		return nil, fmt.Errorf("loading graph files: %w", err)
	}
	states := make([]conceptState, 0, len(concepts))
	for _, c := range concepts {
		if c.Name == "architecture" {
			continue
		}
		states = append(states, conceptState{Name: c.Name, Status: conceptStatus(root, c, recorded)})
	}
	return states, nil
}

// uncoveredFileCount returns how many files in the graph are linked to no
// concept — new code the concept index has not clustered yet. graph update adds
// files to the graph on every session, but only learn clusters them, so this
// count grows between learn runs and is a stable "the concept index is behind
// the code" signal (unlike per-file staleness, which graph update keeps
// clearing). Returns 0 when there is no graph or no concepts yet: a repo that
// never learned is a different state, not drift.
func uncoveredFileCount(root string) (int, error) {
	if _, err := os.Stat(graphDBPath(root)); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	st, err := openGraphStore()
	if err != nil {
		return 0, err
	}
	defer st.Close()
	concepts, err := st.ListConcepts()
	if err != nil {
		return 0, fmt.Errorf("uncoveredFileCount: %w", err)
	}
	if len(concepts) == 0 {
		return 0, nil
	}
	covered := make(map[string]bool)
	for _, c := range concepts {
		if c.Name == "architecture" {
			continue
		}
		for _, f := range c.Files {
			covered[f] = true
		}
	}
	files, err := st.Files()
	if err != nil {
		return 0, fmt.Errorf("uncoveredFileCount: %w", err)
	}
	n := 0
	for path := range files {
		if !covered[path] {
			n++
		}
	}
	return n, nil
}

// conceptStatus reports one concept's freshness: "new" when it has no generated
// SKILL.md yet, "up-to-date" when every member file still hashes to what the
// graph recorded, "stale" otherwise.
func conceptStatus(root string, c store.ConceptRow, recorded map[string]store.FileRecord) string {
	if _, err := os.Stat(filepath.Join(generatedSkillsDir(root), c.Name, "SKILL.md")); err != nil {
		return "new"
	}
	if len(c.Files) == 0 {
		// No member files linked, so there is no evidence the skill still
		// matches the code. Report "stale" rather than claim freshness we
		// cannot verify — a concept learned before the link existed then
		// prompts a re-learn instead of lying.
		return "stale"
	}
	for _, rel := range c.Files {
		if recorded[rel].SHA256 != liveSHA256(root, rel) {
			return "stale"
		}
	}
	return "up-to-date"
}

// liveSHA256 hashes rel's current content exactly the way the graph extractor
// does (lowercase hex of the sha256 over the raw bytes), so a value equal to
// the store's means the file has not changed since the graph was built. A file
// that cannot be read yields a marker no real hash can equal, which reads as
// changed.
func liveSHA256(root, rel string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "<unreadable>"
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// runStatusTo renders the knowledge-index (skill staleness) report to w and
// returns an error describing why the report could not be produced (e.g. no
// concepts). Callers that need to surface that error inline (the top-level
// `status` command) pass their own writer; `learn status` writes to os.Stdout
// via runStatus.
func runStatusTo(w io.Writer, root string) error {
	states, err := conceptStates(root)
	if err != nil {
		return err
	}
	needRefresh := 0
	for _, s := range states {
		fmt.Fprintf(w, "  %-30s %s\n", s.Name, s.Status)
		if s.Status != "up-to-date" {
			needRefresh++
		}
	}
	if needRefresh > 0 {
		fmt.Fprintf(w, "\n%d skill(s) need refresh — re-run 'tu-agent learn <path>' for the changed area, then 'tu-agent learn synthesize'.\n", needRefresh)
	} else {
		fmt.Fprintln(w, "\nAll skills up to date.")
	}
	if orphans, err := codegen.ListEmptySkillDirs(generatedSkillsDir(root)); err == nil && len(orphans) > 0 {
		fmt.Fprintf(w, "\n%d empty skill dir(s) with no SKILL.md: %v\n", len(orphans), orphans)
		fmt.Fprintln(w, "Run 'tu-agent skill prune' to remove them.")
	}
	return nil
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report which skills are stale (their member files changed since the graph was built)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(".")
	},
}

func init() {
	synthesizeCmd.Flags().StringVar(&synthesizeProvider, "provider", "", "provider override (claude|local)")
	learnCmd.AddCommand(synthesizeCmd)
	learnCmd.AddCommand(statusCmd)
}
