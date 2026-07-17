package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/mutation"
	"github.com/carlosneir4/tu-agent/internal/orchestrator"
	"github.com/carlosneir4/tu-agent/internal/subagent"
	"github.com/carlosneir4/tu-agent/internal/tdd"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/testresult"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

var tddProviderOverride string
var tddDesign string
var tddTicket string

var tddCmd = &cobra.Command{
	GroupID: "feature",
	Use:     "tdd",
	Short:   "TDD dev-flow orchestrator (analyst -> architect -> gate -> TDD -> judge)",
}

var tddRunCmd = &cobra.Command{
	Use:   "run [task description...]",
	Short: "Run one feature end-to-end through the TDD dev-flow",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		prov, err := selectProvider(cfg, "tdd", tddProviderOverride)
		if err != nil {
			return err
		}
		idx, err := buildSkillIndex()
		if err != nil {
			return fmt.Errorf("skill index: %w", err)
		}
		root := repoRoot()

		tel, err := telemetry.NewLogger(telemetryPath(root))
		if err != nil {
			return fmt.Errorf("telemetry init: %w", err)
		}

		memStore, err := memory.Open(memoryDBPath(root))
		if err != nil {
			return fmt.Errorf("memory init: %w", err)
		}
		defer func() {
			if cerr := memStore.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()

		// Shared tool registry (same set as chat.go, minus dispatch_agent).
		tools := tool.NewRegistry()
		tools.Register(tool.NewBashTool())
		tools.Register(tool.NewReadFileTool(root))
		tools.Register(tool.NewWriteFileTool(root))
		tools.Register(tool.NewGrepTool(root))
		tools.Register(tool.NewFindTool(root))
		tools.Register(tool.NewListDirTool())
		tools.Register(tool.NewLoadSkillTool(idx, tel))
		tools.Register(tool.NewMemSaveTool(memStore, gitAuthor()))
		tools.Register(tool.NewMemSearchTool(memStore))
		tools.Register(tool.NewMemRecentTool(memStore))

		slug := tdd.Slugify(strings.Join(args, " "))
		relBase := tdd.TddRelBase(tddTicket, slug)
		workDir := filepath.Join(root, relBase)
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		if tddTicket != "" {
			tdd.WarnBranch(tdd.CurrentBranch(root), tdd.SanitizeTicket(tddTicket), cmd.OutOrStdout())
		}

		// Stage dispatcher for architect/craftsman/judge.
		defs, err := tdd.TddStageDefs(root, relBase)
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		dispatcher := subagent.NewDispatcher(defs, prov, tools, tel, idx)

		// Analyst orchestrator (interactive foreground).
		analystBody, err := tdd.LoadAgentBody(root, "analyst")
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		analyst := orchestrator.New(prov, tools, tel, analystBody+"\n\n"+tdd.WithBaseDir(tdd.AnalystPrompt, relBase), "analyst")

		task := "Help me build the following. Interrogate me until the spec is complete.\n\n" + strings.Join(args, " ")
		if tddDesign != "" {
			task = "Help me build the following.\n\n" + strings.Join(args, " ")
		}

		runner, err := tdd.ResolveTestRunner(cfg, root)
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}

		var mutator tdd.Mutator
		threshold := 0.0
		if cfg.Tdd.Mutation {
			threshold = cfg.Tdd.MutationThreshold
			if threshold <= 0 {
				threshold = 0.7
			}
			mutator = func(ctx context.Context, t tdd.MutationTarget) tdd.MutationOutcome {
				eng, ok := mutation.EngineFor(t.Language)
				if !ok {
					return tdd.MutationOutcome{Skipped: true, Note: "no mutation engine for " + t.Language}
				}
				rep := mutation.Run(ctx, eng, root, t.Dir, mutationRunner, 10*time.Minute)
				out := tdd.MutationOutcome{Skipped: rep.Skipped, Score: rep.Score, Note: rep.Note}
				for _, s := range rep.Survivors {
					out.Survivors = append(out.Survivors, fmt.Sprintf("%s:%d %s", s.File, s.Line, s.Desc))
				}
				return out
			}
		}

		archive := cfg.Tdd.Archive == nil || *cfg.Tdd.Archive

		res, err := tdd.Run(cmd.Context(), tdd.Options{
			Analyst:    analyst,
			Dispatcher: dispatcher,
			Runner:     runner,
			In:         cmd.InOrStdin(),
			Out:        cmd.OutOrStdout(),
			Task:       task,
			WorkDir:    workDir,
			RelBase:    relBase,
			FeatureReader: func(name string) (string, error) {
				data, rerr := os.ReadFile(filepath.Join(workDir, "features", name+".feature"))
				return string(data), rerr
			},
			Budget:            3,
			Mutator:           mutator,
			MutationThreshold: threshold,
			Archive:           archive,
			Strict:            cfg.Tdd.Strict,
			DesignDoc:         tddDesign,
			Snapshot:          func(ctx context.Context) (string, error) { return tdd.Snapshot(ctx, root) },
			Diff: func(ctx context.Context, from, to string) ([]string, error) {
				return tdd.DiffFiles(ctx, root, from, to)
			},
			LoadReports: func(since time.Time) (testresult.Report, error) {
				return testresult.LoadReports(root, since)
			},
			ReviewScope: func(ctx context.Context, _ string) (string, []string, string, error) {
				return tdd.ReviewScope(ctx, root)
			},
		})
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\ntdd finished: %s\n", res.Status)
		for _, f := range res.Features {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s: %s\n", f.Name, f.Status)
		}
		return nil
	},
}

func init() {
	tddRunCmd.Flags().StringVar(&tddProviderOverride, "provider", "",
		"override provider for this run (claude|local); empty uses routing config")
	tddRunCmd.Flags().StringVar(&tddDesign, "design", "",
		"path to a design doc or superpowers plan the analyst seeds the spec from (confirm-by-exception)")
	tddRunCmd.Flags().StringVar(&tddTicket, "ticket", "",
		"ticket id; groups artifacts under .tu-agent/tdd/<ticket>-<slug>/ and warns if the branch doesn't match")
	tddCmd.AddCommand(tddRunCmd)
	rootCmd.AddCommand(tddCmd)
}
