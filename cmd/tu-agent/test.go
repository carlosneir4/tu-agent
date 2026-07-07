package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/coverage"
	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/query"
	"github.com/tu/tu-agent/internal/mutation"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/testgen"
)

// gapJSON is the stable machine-readable shape of one gap.
// Field names are a compatibility contract — extend, never rename.
type gapJSON struct {
	Symbol      string   `json:"symbol"`
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Signature   string   `json:"signature"`
	FanIn       int      `json:"fan_in"`
	BlastRadius int      `json:"blast_radius"`
	Span        int      `json:"span"`
	Score       float64  `json:"score"`
	Covered     *float64 `json:"covered,omitempty"`
}

// gapGate is the result of the --fail-under coverage gate. It is computed only
// when the gate is requested (failUnder > 0) and a coverage profile is present.
type gapGate struct {
	Covered, Known int
	Pct            float64
	Threshold      float64
	Below          bool
}

// Summary renders the one-line gate result for stderr / the MCP result text.
func (g gapGate) Summary() string {
	status := "PASS"
	if g.Below {
		status = "FAIL"
	}
	return fmt.Sprintf("Coverage gate: %.1f%% covered (%d/%d lines), threshold %.1f%% — %s",
		g.Pct, g.Covered, g.Known, g.Threshold, status)
}

func runTestGaps(domain string, top, minLines, depth int, asJSON bool, coveragePath string, cover bool, failUnder float64) (string, *gapGate, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", nil, err
	}
	var cov query.SymbolCoverer
	var profile coverage.Profile
	if coveragePath != "" {
		prof, err := coverage.LoadAuto(coveragePath, modulePath(), repoRoot())
		if err != nil {
			slog.Warn("test gaps: coverage report unusable, falling back to graph proxy", "err", err)
		} else {
			cov, profile = prof, prof
		}
	} else if cover {
		lang := primaryLanguage(g)
		var prof coverage.Profile
		var gerr error
		if lang == coverage.LangTS {
			var npkgs int
			prof, npkgs, gerr = generateTSGapsCoverage(g, domain)
			if gerr == nil && failUnder > 0 && npkgs > 1 {
				return "", nil, fmt.Errorf("test gaps: --fail-under is not supported for multi-package TypeScript coverage (%d packages); narrow with --domain to a single package", npkgs)
			}
		} else {
			prof, gerr = coverage.Generate(lang, repoRoot(), modulePath(), coverage.ExecRunner)
		}
		if gerr != nil {
			slog.Warn("test gaps: coverage generation failed, falling back to graph proxy", "err", gerr)
		} else {
			cov, profile = prof, prof
		}
	}
	gaps, err := g.UntestedGaps(query.GapOptions{
		Domain: domain, Top: top, MinLines: minLines, Depth: depth, Coverage: cov,
	})
	if err != nil {
		return "", nil, err
	}

	var out string
	if !asJSON {
		out = query.FormatGaps(gaps)
	} else {
		js := make([]gapJSON, 0, len(gaps))
		for _, gp := range gaps {
			sig := gp.Node.Params
			if gp.Node.ReturnType != "" {
				sig += " " + gp.Node.ReturnType
			}
			gj := gapJSON{
				Symbol: gp.Node.Name, ID: gp.Node.ID, File: gp.Node.Path, Line: gp.Node.Line,
				Signature: sig, FanIn: gp.FanIn, BlastRadius: gp.BlastRadius,
				Span: gp.Span, Score: gp.Score,
			}
			if gp.Covered >= 0 {
				c := gp.Covered
				gj.Covered = &c
			}
			js = append(js, gj)
		}
		data, err := json.MarshalIndent(js, "", "  ")
		if err != nil {
			return "", nil, fmt.Errorf("runTestGaps: marshal: %w", err)
		}
		out = string(data) + "\n"
	}

	// --fail-under: gates on the overall coverage of the supplied report. For TS,
	// --cover + --fail-under with multiple packages is rejected above; single-package
	// TS (possibly scoped via --domain) and all non-TS cases continue here.
	var gate *gapGate
	if failUnder > 0 {
		if profile == nil {
			return "", nil, fmt.Errorf("test gaps: --fail-under requires a coverage source (--coverage or --cover)")
		}
		covered, known, ratio := profile.Overall()
		pct := ratio * 100
		gate = &gapGate{Covered: covered, Known: known, Pct: pct, Threshold: failUnder, Below: pct < failUnder}
	}
	return out, gate, nil
}

// primaryLanguage returns the most common language among function nodes,
// defaulting to "go".
func primaryLanguage(g *query.Graph) string {
	counts := map[string]int{}
	for _, n := range g.FunctionLanguages() {
		counts[n]++
	}
	best, bestN := "go", 0
	for lang, n := range counts {
		if n > bestN && lang != "" {
			best, bestN = lang, n
		}
	}
	return best
}

// generateTSGapsCoverage runs per-package TS coverage for every distinct
// package that contains a TypeScript function node (optionally scoped to
// domain), unioning the profiles. It resolves each package's framework with a
// fresh testgen.TSAdapter. Any single package failure is logged and skipped;
// the union of the rest is returned together with the count of packages that
// succeeded.
func generateTSGapsCoverage(g *query.Graph, domain string) (coverage.Profile, int, error) {
	root := repoRoot()
	files, err := g.TSFunctionFiles(domain)
	if err != nil {
		return nil, 0, fmt.Errorf("generateTSGapsCoverage: %w", err)
	}
	pkgs := map[string]string{} // pkgDir -> framework
	ad := &testgen.TSAdapter{}
	for _, n := range files {
		t := testgen.Target{Path: n, Language: "typescript"}
		pkgDir, framework := ad.ResolveForCoverage(root, t)
		if _, seen := pkgs[pkgDir]; !seen {
			pkgs[pkgDir] = framework
		}
	}
	if len(pkgs) == 0 {
		return nil, 0, fmt.Errorf("generateTSGapsCoverage: no TypeScript packages found")
	}
	merged := coverage.Profile{}
	succeeded := 0
	for pkgDir, framework := range pkgs {
		prof, err := coverage.GenerateTS(root, pkgDir, framework, coverage.ExecRunner)
		if err != nil {
			slog.Warn("test gaps: package coverage failed, skipping", "pkg", pkgDir, "err", err)
			continue
		}
		merged.Merge(prof)
		succeeded++
	}
	if succeeded == 0 {
		return nil, 0, fmt.Errorf("generateTSGapsCoverage: all package coverage runs failed")
	}
	return merged, succeeded, nil
}

// modulePath reads the module path from go.mod (empty when absent).
func modulePath() string {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "go.mod"))
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(ln), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

var (
	testGapsDomain    string
	testGapsTop       int
	testGapsJSON      bool
	testGapsMinLines  int
	testGapsDepth     int
	testGapsCoverage  string
	testGapsCover     bool
	testGapsFailUnder float64
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test-coverage analysis backed by the knowledge graph",
}

var testGapsCmd = &cobra.Command{
	Use:   "gaps",
	Short: "Rank untested public functions by risk (fan-in × blast radius)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, gate, err := runTestGaps(testGapsDomain, testGapsTop, testGapsMinLines, testGapsDepth, testGapsJSON, testGapsCoverage, testGapsCover, testGapsFailUnder)
		if err != nil {
			return err
		}
		fmt.Print(out)
		if gate != nil {
			fmt.Fprintln(os.Stderr, gate.Summary())
			if gate.Below {
				return fmt.Errorf("coverage gate failed: %.1f%% < %.1f%%", gate.Pct, gate.Threshold)
			}
		}
		return nil
	},
}

// mutationThreshold: annotate generated tests when the mutation score is below
// this (any surviving mutant). Advisory only — never triggers regeneration.
const mutationThreshold = 1.0

// mutationNote decides whether a passed target's generated tests should carry a
// MUTATION annotation, and renders the note. A skipped report never annotates.
func mutationNote(rep mutation.Report) (string, bool) {
	if rep.Skipped || rep.Score >= mutationThreshold {
		return "", false
	}
	return fmt.Sprintf("score %.1f%% — %d survivor(s); see `tu-agent test mutation`", rep.Score*100, rep.Survived), true
}

var (
	testGenDryRun    bool
	testGenMaxRepair int
	testGenDiscard   bool
	testGenProvider  string
	testGenTimeout   int
	testGenTop       int
	testGenDomain    string
	testGenMutate    bool
)

// resolveTestGenTargets resolves the CLI inputs into the ordered list of
// function targets to generate. Exactly one of {target, top>0} drives it
// (the caller validates that). A class target expands to its exported
// methods; --top drains the highest-risk gaps.
func resolveTestGenTargets(target string, top int, domain string) (*query.Graph, []testgen.Target, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return nil, nil, err
	}
	if top > 0 {
		gaps, err := g.UntestedGaps(query.GapOptions{Top: top, Domain: domain, MinLines: 4, Depth: 2})
		if err != nil {
			return nil, nil, err
		}
		targets := make([]testgen.Target, 0, len(gaps))
		for _, gp := range gaps {
			targets = append(targets, testgen.TargetFromNode(gp.Node))
		}
		if len(targets) == 0 {
			return nil, nil, fmt.Errorf("test gen: no untested gaps to generate for")
		}
		return g, targets, nil
	}

	id, _, err := resolveTargetChecked(g, target)
	if err != nil {
		return nil, nil, err
	}
	node, ok := g.NodeByID(id)
	if !ok {
		return nil, nil, fmt.Errorf("test gen: target %q not found — try 'tu-agent graph find %s'", target, target)
	}
	switch node.Kind {
	case graph.KindFunction:
		return g, []testgen.Target{testgen.TargetFromNode(node)}, nil
	case graph.KindClass:
		methods := g.ClassMethods(node.ID)
		if len(methods) == 0 {
			return nil, nil, fmt.Errorf("test gen: class %s has no exported methods to generate for", node.ID)
		}
		targets := make([]testgen.Target, 0, len(methods))
		for _, m := range methods {
			targets = append(targets, testgen.TargetFromNode(m))
		}
		return g, targets, nil
	default:
		return nil, nil, fmt.Errorf("test gen: %s is a %s — only function or class targets are supported", node.ID, node.Kind)
	}
}

func runTestGen(ctx context.Context, target string, top int, domain string) error {
	if (target == "") == (top <= 0) {
		return fmt.Errorf("test gen: pass exactly one of a <target> or --top N")
	}
	g, targets, err := resolveTestGenTargets(target, top, domain)
	if err != nil {
		return err
	}
	prov, err := selectProvider(cfg, "test_gen", testGenProvider)
	if err != nil {
		return err
	}
	tel, err := telemetry.NewLogger(filepath.Join(repoRoot(), ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}
	opts := testgen.Options{
		RepoRoot:       ".",
		MaxRepair:      testGenMaxRepair,
		Timeout:        time.Duration(testGenTimeout) * time.Second,
		DryRun:         testGenDryRun,
		DiscardFailing: testGenDiscard,
	}
	rep := testgen.GenerateBatch(ctx, g, prov, tel, nil, targets, opts)
	fmt.Print(formatBatch(rep, testGenDryRun))
	if testGenMutate && !testGenDryRun {
		for _, it := range rep.Items {
			if it.Result == nil || !it.Result.Passed {
				continue
			}
			eng, ok := mutation.EngineFor(it.Target.Language)
			if !ok {
				continue
			}
			pkgDir := filepath.Dir(it.Target.Path)
			mrep := mutation.Run(ctx, eng, repoRoot(), pkgDir, mutationRunner, 10*time.Minute)
			fmt.Print(formatMutation(mrep))
			if note, ok := mutationNote(mrep); ok {
				abs := filepath.Join(".", it.Result.TestPath)
				data, rerr := os.ReadFile(abs)
				if rerr != nil {
					slog.Warn("test gen --mutate: could not read test file to annotate", "file", it.Result.TestPath, "err", rerr)
				} else {
					annotated := testgen.AnnotateMutation(it.Target.Language, string(data), note)
					if werr := os.WriteFile(abs, []byte(annotated), 0o644); werr != nil {
						slog.Warn("test gen --mutate: could not annotate", "file", it.Result.TestPath, "err", werr)
					}
				}
			}
		}
	}
	if !testGenDryRun && rep.Passed < len(rep.Items) {
		return fmt.Errorf("test gen: %d of %d target(s) did not pass", len(rep.Items)-rep.Passed, len(rep.Items))
	}
	return nil
}

// formatBatch renders a batch report. In dry-run it prints the would-write /
// would-run header and generated code for each target; otherwise one status
// line per target plus a summary.
func formatBatch(rep testgen.BatchReport, dryRun bool) string {
	var b strings.Builder
	if dryRun {
		for _, it := range rep.Items {
			if it.Result == nil {
				fmt.Fprintf(&b, "// %s: %v\n", it.Target.Name, it.Err)
				continue
			}
			fmt.Fprintf(&b, "// would write: %s\n// would run:   %s\n\n%s\n\n",
				it.Result.TestPath, strings.Join(it.Result.RunCommand, " "), it.Result.Code)
		}
		return b.String()
	}
	for _, it := range rep.Items {
		switch {
		case it.Result != nil && it.Result.Passed:
			fmt.Fprintf(&b, "PASS  %s  %s  (%s)\n", it.Result.TestPath, it.Target.Name, attempts(it.Result.Attempts))
		case it.Result != nil && it.Result.FIXME:
			fmt.Fprintf(&b, "FIXME %s  %s  (%s)\n", it.Result.TestPath, it.Target.Name, attempts(it.Result.Attempts))
		case it.Result != nil && it.Result.Discarded:
			fmt.Fprintf(&b, "DISCARD %s  %s  (%s)\n", it.Result.TestPath, it.Target.Name, attempts(it.Result.Attempts))
		default:
			fmt.Fprintf(&b, "ERROR %s  %v\n", it.Target.Name, it.Err)
		}
	}
	fmt.Fprintf(&b, "Summary: %d passed, %d FIXME, %d discarded, %d errored (%d targets)\n",
		rep.Passed, rep.FIXMEd, rep.Discarded, rep.Errored, len(rep.Items))
	return b.String()
}

// attempts renders an attempt count with correct pluralization.
func attempts(n int) string {
	if n == 1 {
		return "1 attempt"
	}
	return fmt.Sprintf("%d attempts", n)
}

// mutationRunner runs argv in dir under a timeout — the real subprocess path
// for mutation engines.
func mutationRunner(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("mutationRunner: empty argv")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runTestMutation resolves a target, selects the engine by its language, runs
// mutation over the target's package directory, and formats the report. It
// degrades (never crashes) when the engine or tool is unavailable.
// targets[0] is sufficient because mutation is scoped to the package directory
// (all methods of a class live in the same source file/package).
func runTestMutation(ctx context.Context, target string) (string, error) {
	_, targets, err := resolveTestGenTargets(target, 0, "")
	if err != nil {
		return "", err
	}
	tgt := targets[0]
	eng, ok := mutation.EngineFor(tgt.Language)
	if !ok {
		return fmt.Sprintf("Mutation unsupported for language %q\n", tgt.Language), nil
	}
	pkgDir := filepath.Dir(tgt.Path)
	rep := mutation.Run(ctx, eng, repoRoot(), pkgDir, mutationRunner, 10*time.Minute)
	return formatMutation(rep), nil
}

// formatMutation renders a mutation report for the terminal / MCP result.
func formatMutation(rep mutation.Report) string {
	if rep.Skipped {
		return fmt.Sprintf("Mutation skipped: %s\n", rep.Note)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mutation (%s): score %.1f%% — %d/%d killed, %d survived\n",
		rep.Tool, rep.Score*100, rep.Killed, rep.Total, rep.Survived)
	for _, s := range rep.Survivors {
		if s.Line > 0 {
			fmt.Fprintf(&b, "  %s:%d  %s\n", s.File, s.Line, s.Desc)
		} else {
			fmt.Fprintf(&b, "  %s  %s\n", s.File, s.Desc)
		}
	}
	return b.String()
}

var testMutationCmd = &cobra.Command{
	Use:   "mutation <target>",
	Short: "Run mutation testing on a symbol's package (opt-in; requires an external engine)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runTestMutation(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

// runTestScaffold returns the deterministic half of test gen as a JSON array —
// one scaffold per target. A function yields one; a class yields one per
// exported method. The plugin skill generates each with Claude Code and runs
// the same scoped verification (spec decision 5).
func runTestScaffold(target string) (string, error) {
	g, targets, err := resolveTestGenTargets(target, 0, "")
	if err != nil {
		return "", err
	}
	scaffolds := make([]*testgen.Scaffold, 0, len(targets))
	for _, tgt := range targets {
		ad, err := testgen.AdapterFor(tgt.Language)
		if err != nil {
			return "", err
		}
		sc, err := testgen.BuildScaffold(g, ad, ".", tgt, 0)
		if err != nil {
			return "", err
		}
		scaffolds = append(scaffolds, sc)
	}
	data, err := json.MarshalIndent(scaffolds, "", "  ")
	if err != nil {
		return "", fmt.Errorf("runTestScaffold: marshal: %w", err)
	}
	return string(data) + "\n", nil
}

var testGenCmd = &cobra.Command{
	Use:   "gen [target]",
	Short: "Generate verified unit tests for a symbol, a whole class, or the top-N risk gaps",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		return runTestGen(cmd.Context(), target, testGenTop, testGenDomain)
	},
}

func init() {
	testGapsCmd.Flags().StringVar(&testGapsDomain, "domain", "", "restrict to files of one domain skill")
	testGapsCmd.Flags().IntVar(&testGapsTop, "top", 20, "show only the N highest-risk symbols (0 = all)")
	testGapsCmd.Flags().BoolVar(&testGapsJSON, "json", false, "machine-readable JSON output")
	testGapsCmd.Flags().IntVar(&testGapsMinLines, "min-lines", 4, "exclude functions spanning fewer lines")
	testGapsCmd.Flags().IntVar(&testGapsDepth, "depth", 2, "BFS depth for the blast radius")
	testGapsCmd.Flags().StringVar(&testGapsCoverage, "coverage", "", "path to a coverage report (go/jacoco/coverage.py/istanbul; format auto-detected)")
	testGapsCmd.Flags().BoolVar(&testGapsCover, "cover", false, "generate coverage by running the suite, then rank by it")
	testGapsCmd.Flags().Float64Var(&testGapsFailUnder, "fail-under", 0, "exit non-zero if overall covered% is below this threshold (requires --coverage or --cover)")
	testCmd.AddCommand(testGapsCmd)
	rootCmd.AddCommand(testCmd)
	testGenCmd.Flags().BoolVar(&testGenDryRun, "dry-run", false, "print the generated test without writing or running it")
	testGenCmd.Flags().IntVar(&testGenMaxRepair, "max-repair", testgen.DefaultMaxRepair, "repair attempts after a failed verification")
	testGenCmd.Flags().BoolVar(&testGenDiscard, "discard-failing", false, "delete the test instead of leaving a FIXME marker when verification fails")
	testGenCmd.Flags().StringVar(&testGenProvider, "provider", "", "override provider for this run (claude|local)")
	testGenCmd.Flags().IntVar(&testGenTimeout, "timeout", 120, "scoped test-run timeout in seconds")
	testGenCmd.Flags().IntVar(&testGenTop, "top", 0, "generate for the top-N highest-risk untested gaps instead of a named target")
	testGenCmd.Flags().StringVar(&testGenDomain, "domain", "", "with --top, restrict gaps to one domain skill")
	testGenCmd.Flags().BoolVar(&testGenMutate, "mutate", false, "after a test passes, run mutation testing and annotate weak generated tests (opt-in; needs an external engine)")
	testCmd.AddCommand(testGenCmd)
	testCmd.AddCommand(testMutationCmd)
}
