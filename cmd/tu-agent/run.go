package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/tu/tu-agent/internal/memory"
	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/subagent"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

var (
	runTask     string
	runProvider string
	runTelPath  string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute a single task non-interactively and exit",
	Long: `Runs a single task through the agent loop and prints the response to stdout.
Designed for scripted benchmarks. Exits after one task completes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runTask == "" {
			return fmt.Errorf("--task is required")
		}
		prov, err := selectProvider(cfg, "run", runProvider)
		if err != nil {
			return err
		}

		telPath := runTelPath
		if telPath == "" {
			telPath = ".tu-agent/telemetry.jsonl"
		}
		tel, err := telemetry.NewLogger(telPath)
		if err != nil {
			return fmt.Errorf("telemetry init: %w", err)
		}

		idx, err := buildSkillIndex()
		if err != nil {
			return fmt.Errorf("skill index: %w", err)
		}

		agentDefs, err := buildAgentDefs()
		if err != nil {
			return fmt.Errorf("agent loader: %w", err)
		}

		memStore, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return fmt.Errorf("memory init: %w", err)
		}
		defer func() {
			if cerr := memStore.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		tools := tool.NewRegistry()
		tools.Register(tool.NewBashTool())
		tools.Register(tool.NewReadFileTool(cwd))
		tools.Register(tool.NewWriteFileTool(cwd))
		tools.Register(tool.NewGrepTool(cwd))
		tools.Register(tool.NewFindTool(cwd))
		tools.Register(tool.NewListDirTool())
		tools.Register(tool.NewLoadSkillTool(idx, tel))
		tools.Register(tool.NewMemSaveTool(memStore, gitAuthor()))
		tools.Register(tool.NewMemSearchTool(memStore))
		tools.Register(tool.NewMemRecentTool(memStore))

		dispatcher := subagent.NewDispatcher(agentDefs, prov, tools, tel, idx)
		tools.Register(subagent.NewDispatchAgentTool(dispatcher))

		// Memory tools are registered but not injected into the system prompt;
		// run is designed for reproducible benchmark tasks, not session continuity.
		systemPrompt := buildSystemPrompt(idx)
		if suffix := loadPromptSuffix(".", "run", prov.Name()); suffix != "" {
			systemPrompt += "\n\n## Provider Hints\n\n" + suffix
		}
		orch := orchestrator.New(prov, tools, tel, systemPrompt, "")
		response, err := orch.Chat(cmd.Context(), runTask)
		if err != nil {
			return fmt.Errorf("run: %w", err)
		}
		fmt.Println(response)
		return nil
	},
}

func init() {
	runCmd.Flags().StringVar(&runTask, "task", "", "task prompt to execute (required)")
	runCmd.Flags().StringVar(&runProvider, "provider", "", "provider override (claude|local)")
	runCmd.Flags().StringVar(&runTelPath, "telemetry", "", "telemetry output file (default: .tu-agent/telemetry.jsonl)")
}
