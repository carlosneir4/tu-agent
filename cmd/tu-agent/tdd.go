package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/memory"
	"github.com/tu/tu-agent/internal/mutation"
	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/subagent"
	"github.com/tu/tu-agent/internal/tdd"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/testresult"
	"github.com/tu/tu-agent/internal/tool"
)

var tddProviderOverride string

// tddStage pairs a flow stage with the agent-file role that supplies its
// project knowledge, the generic TDD overlay, and the stage's tool grant.
type tddStage struct {
	stage   string
	role    string
	overlay string
	tools   []string
}

// tddStages is the single mapping of flow stages onto init-provisioned agents.
func tddStages() []tddStage {
	writeGrant := append([]string{}, tdd.CraftsmanToolGrant...) // Default + write_file
	defaultGrant := append([]string{}, tdd.DefaultToolGrant...)
	return []tddStage{
		{"analyst", "analyst", tdd.AnalystPrompt, writeGrant},
		{"architect", "architect", tdd.ArchitectPrompt, writeGrant},
		{"craftsman", "developer", tdd.CraftsmanPrompt, writeGrant},
		{"judge", "pr-reviewer", tdd.JudgePrompt, writeGrant},
		{"scribe", "scribe", tdd.ScribePrompt, defaultGrant},
		// test-writer/implementer are not execution stages — Run/runFeatureTDD
		// (internal/tdd/flow.go) dispatch the sandwich via the "craftsman" stage
		// name only. These two exist solely so the plugin conductor can fetch
		// their composed prompt via `tu-agent tdd prompt <name>`.
		{"test-writer", "developer", tdd.TestWriterPrompt, writeGrant},
		{"implementer", "developer", tdd.ImplementerPrompt, writeGrant},
	}
}

// validateTddAgents returns the roles whose .claude/agents/<role>.md file is
// missing. Empty means the flow can run; otherwise the caller tells the user to
// run `tu-agent init`. Roles are deduplicated: test-writer/implementer share the
// "developer" role with craftsman, so a missing developer.md is reported once.
func validateTddAgents(root string) []string {
	var missing []string
	seen := make(map[string]bool)
	for _, st := range tddStages() {
		if seen[st.role] {
			continue
		}
		seen[st.role] = true
		if _, err := os.Stat(filepath.Join(root, ".claude", "agents", st.role+".md")); err != nil {
			missing = append(missing, st.role)
		}
	}
	return missing
}

// loadAgentBody returns the markdown body of .claude/agents/<role>.md with the
// YAML frontmatter stripped — the role's durable project knowledge.
func loadAgentBody(root, role string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "agents", role+".md"))
	if err != nil {
		return "", fmt.Errorf("loadAgentBody(%s): %w", role, err)
	}
	return stripFrontmatter(string(data)), nil
}

// stripFrontmatter removes a leading `---`…`---` YAML block if present.
func stripFrontmatter(s string) string {
	t := strings.TrimLeft(s, "\n")
	if !strings.HasPrefix(t, "---\n") {
		return s
	}
	rest := t[len("---\n"):]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		return strings.TrimLeft(rest[i+len("\n---"):], "\n")
	}
	return s
}

// tddStageDefs builds the dispatched stage definitions (architect, craftsman,
// judge, scribe) whose system prompt = agent body + stage overlay. The analyst
// runs in the foreground and is built separately.
func tddStageDefs(root string) ([]*subagent.Definition, error) {
	var defs []*subagent.Definition
	for _, st := range tddStages() {
		if st.stage == "analyst" {
			continue
		}
		body, err := loadAgentBody(root, st.role)
		if err != nil {
			return nil, err
		}
		defs = append(defs, &subagent.Definition{
			Name:         st.stage,
			Description:  st.stage,
			SystemPrompt: body + "\n\n" + st.overlay,
			ToolSubset:   append([]string{}, st.tools...),
		})
	}
	return defs, nil
}

// resolveTestRunner builds the deterministic gate's TestRunner. Resolution:
//  1. cfg.Tdd.TestCommand set    -> run it via `sh -c`.
//  2. empty + go.mod at repoRoot -> default `go test ./...`.
//  3. empty + no go.mod          -> error (caller must fail fast).
func resolveTestRunner(cfg config.Config, repoRoot string) (tdd.TestRunner, error) {
	if cmd := strings.TrimSpace(cfg.Tdd.TestCommand); cmd != "" {
		return func(ctx context.Context) (bool, string, error) {
			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Dir = repoRoot
			return runTestCmd(c)
		}, nil
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
		return func(ctx context.Context) (bool, string, error) {
			c := exec.CommandContext(ctx, "go", "test", "./...")
			c.Dir = repoRoot
			return runTestCmd(c)
		}, nil
	}
	return nil, fmt.Errorf("no test command configured; set tdd.test_command in .tu-agent/config.yaml")
}

// runTestCmd runs cmd and maps the outcome onto the TestRunner contract:
// an *exec.ExitError means the tests ran and failed; any other error means
// the command could not run.
func runTestCmd(cmd *exec.Cmd) (bool, string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, string(out), nil // tests ran and failed
		}
		return false, string(out), err // could not run
	}
	return true, string(out), nil
}

var tddCmd = &cobra.Command{
	Use:   "tdd",
	Short: "TDD dev-flow orchestrator (analyst -> architect -> gate -> TDD -> judge)",
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

		if missing := validateTddAgents(root); len(missing) > 0 {
			return fmt.Errorf("tdd run: missing dev-flow agents in .claude/agents/ (%s) — run `tu-agent init`", strings.Join(missing, ", "))
		}

		tel, err := telemetry.NewLogger(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
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

		// Stage dispatcher for architect/craftsman/judge.
		defs, err := tddStageDefs(root)
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		dispatcher := subagent.NewDispatcher(defs, prov, tools, tel, idx)

		// Analyst orchestrator (interactive foreground).
		analystBody, err := loadAgentBody(root, "analyst")
		if err != nil {
			return fmt.Errorf("tdd run: %w", err)
		}
		analyst := orchestrator.New(prov, tools, tel, analystBody+"\n\n"+tdd.AnalystPrompt, "analyst")

		workDir := filepath.Join(root, ".tu-agent", "tdd")
		task := "Help me build the following. Interrogate me until the spec is complete.\n\n" + strings.Join(args, " ")

		runner, err := resolveTestRunner(cfg, root)
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
			FeatureReader: func(name string) (string, error) {
				data, rerr := os.ReadFile(filepath.Join(workDir, "features", name+".feature"))
				return string(data), rerr
			},
			Budget:            3,
			Mutator:           mutator,
			MutationThreshold: threshold,
			Archive:           archive,
			Strict:            cfg.Tdd.Strict,
			Snapshot:          func(ctx context.Context) (string, error) { return tdd.Snapshot(ctx, root) },
			Diff: func(ctx context.Context, from, to string) ([]string, error) {
				return tdd.DiffFiles(ctx, root, from, to)
			},
			LoadReports: func(since time.Time) (testresult.Report, error) {
				return testresult.LoadReports(root, since)
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
	tddCmd.AddCommand(tddRunCmd)
	rootCmd.AddCommand(tddCmd)
}
