package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/memory"
	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/skill"
	"github.com/tu/tu-agent/internal/subagent"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

const baseSystemPrompt = `You are an AI coding assistant with access to tools.
You can execute bash commands to investigate code, run tests, and help with development tasks.
When a task requires exploring multiple files or investigating a large codebase, use dispatch_agent
to delegate the exploration to codebase-explorer — it runs with a clean context and returns a
structured summary, keeping this conversation focused.
When using tools directly, prefer short commands. Always explain what you found.`

// buildSkillIndex scans the standard skill directories and returns the index.
func buildSkillIndex() (*skill.Index, error) {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		slog.Debug("buildSkillIndex: UserHomeDir failed, using empty string", "err", homeErr)
	}
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		slog.Debug("buildSkillIndex: Getwd failed, using empty string", "err", cwdErr)
	}
	return skill.Scan(skill.SearchPaths(home, cwd))
}

// buildSystemPrompt returns the system prompt with the skill index appended when non-empty.
func buildSystemPrompt(idx *skill.Index) string {
	summary := idx.Summary()
	if summary == "" {
		return baseSystemPrompt
	}
	return baseSystemPrompt + "\n\n## Available Skills\n\n" +
		"Use the load_skill tool to load the full content of a skill when relevant to the task.\n\n" +
		summary
}

// buildAgentDefs loads user-defined sub-agent definitions from the standard directories.
func buildAgentDefs() ([]*subagent.Definition, error) {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		slog.Debug("buildAgentDefs: UserHomeDir failed, using empty string", "err", homeErr)
	}
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		slog.Debug("buildAgentDefs: Getwd failed, using empty string", "err", cwdErr)
	}
	// Agents loaded from the project directory are restricted to read-only tools.
	// This prevents a malicious .claude/agents/ file from claiming bash access.
	readOnlyDirs := map[string]bool{}
	if cwd != "" {
		readOnlyDirs[filepath.Join(cwd, ".claude", "agents")] = true
	}
	return subagent.Load(subagent.SearchPaths(home, cwd), readOnlyDirs)
}

// appendMemorySection appends a "## Recent Memory" section to prompt when obs is non-empty.
func appendMemorySection(prompt string, obs []memory.Observation) string {
	if len(obs) == 0 {
		return prompt
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\n## Recent Memory\n\n")
	sb.WriteString("Observations from previous sessions (most recent last):\n")
	for _, o := range obs {
		fmt.Fprintf(&sb, "\n[%s] %s\n%s\n", o.Title, o.UpdatedAt.Format("2006-01-02"), o.Content)
	}
	return sb.String()
}

// appendLastSessionSummary appends a "## Last session" block to prompt when
// summary is non-empty, so the chat loop opens with continuity from the prior
// session ahead of the recent-observations section. Deterministic; no model call.
func appendLastSessionSummary(prompt, summary string) string {
	if summary == "" {
		return prompt
	}
	return prompt + "\n\n## Last session\n\n" + summary + "\n"
}

// loadPromptSuffix returns the content of the first matching suffix file under
// <dir>/.tu-agent/prompts/, trying <task>-<provider>.md then <provider>.md.
// Returns "" when no file is found.
func loadPromptSuffix(dir, task, provider string) string {
	candidates := []string{
		filepath.Join(dir, ".tu-agent", "prompts", task+"-"+provider+".md"),
		filepath.Join(dir, ".tu-agent", "prompts", provider+".md"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

const defaultLocalBaseURL = "http://localhost:1234"

var providerOverride string

var chatCmd = &cobra.Command{
	Use:        "chat",
	Short:      "Start an interactive agent session in the current repository",
	Deprecated: "the standalone provider harness is frozen; use tu-agent through the Claude Code plugin (see CLAUDE.md §10)",
	RunE: func(cmd *cobra.Command, args []string) error {
		prov, err := selectProvider(cfg, "chat", providerOverride)
		if err != nil {
			return err
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
		recentObs, err := memStore.Recent(10)
		if err != nil {
			return fmt.Errorf("memory recent: %w", err)
		}
		lastSummary, err := memStore.LastSummary("")
		if err != nil {
			return fmt.Errorf("memory last summary: %w", err)
		}

		tel, err := telemetry.NewLogger(".tu-agent/telemetry.jsonl")
		if err != nil {
			return fmt.Errorf("telemetry init: %w", err)
		}

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

		systemPrompt := buildSystemPrompt(idx)
		if suffix := loadPromptSuffix(".", "chat", prov.Name()); suffix != "" {
			systemPrompt += "\n\n## Provider Hints\n\n" + suffix
		}
		orch := orchestrator.New(prov, tools, tel,
			appendMemorySection(appendLastSessionSummary(systemPrompt, lastSummary), recentObs), "")

		memCount, err := memStore.Len()
		if err != nil {
			return fmt.Errorf("memory count: %w", err)
		}
		fmt.Printf("tu-agent chat — provider: %s (%s) — %d skill(s) — %d agent(s) — %d memory — type /exit or Ctrl+D to quit\n",
			prov.Name(), prov.Model(), idx.Len(), dispatcher.Len(), memCount)
		return orch.Run(cmd.Context())
	},
}

func init() {
	chatCmd.Flags().StringVar(&providerOverride, "provider", "",
		"override provider for this session (claude|local); empty uses routing config")
}

// resolveProviderName picks a provider name by precedence:
//
//	override → cfg.Routing.Tasks[task] → cfg.Routing.Default → "claude".
//
// Pure function — no env vars, no I/O — so it is fully covered by unit tests.
func resolveProviderName(cfg config.Config, task, override string) string {
	if override != "" {
		return override
	}
	if r, ok := cfg.Routing.Tasks[task]; ok && r != "" {
		return r
	}
	if cfg.Routing.Default != "" {
		return cfg.Routing.Default
	}
	return "claude"
}

// selectProvider resolves the active provider for a task and constructs it.
// API keys are read from environment variables: ANTHROPIC_API_KEY, LOCAL_API_KEY.
func selectProvider(cfg config.Config, task, override string) (provider.Provider, error) {
	if cfg.Routing.Disabled || os.Getenv("TU_AGENT_NO_PROVIDER") != "" {
		return nil, fmt.Errorf("provider calls are disabled for this repository (routing.disabled in .tu-agent/config.yaml or TU_AGENT_NO_PROVIDER) — deterministic commands are unaffected")
	}
	name := resolveProviderName(cfg, task, override)
	pc := cfg.Providers[name]
	switch name {
	case "claude":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
		}
		return provider.NewClaudeAdapter(provider.ClaudeConfig{
			APIKey:          apiKey,
			BaseURL:         pc.BaseURL,
			Model:           pc.Model,
			MaxOutputTokens: pc.MaxOutputTokens,
		}), nil
	case "local":
		baseURL := pc.BaseURL
		if baseURL == "" {
			baseURL = defaultLocalBaseURL
		}
		var timeout time.Duration
		if pc.RequestTimeoutSeconds > 0 {
			timeout = time.Duration(pc.RequestTimeoutSeconds) * time.Second
		}
		return provider.NewLocalAdapter(provider.LocalConfig{
			APIKey:          os.Getenv("LOCAL_API_KEY"),
			BaseURL:         baseURL,
			Model:           pc.Model,
			MaxOutputTokens: pc.MaxOutputTokens,
			Temperature:     pc.Temperature,
			Timeout:         timeout,
		}), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (configure 'claude' or 'local' in .tu-agent/config.yaml; note: provider 'qwen' was renamed to 'local')", name)
	}
}
