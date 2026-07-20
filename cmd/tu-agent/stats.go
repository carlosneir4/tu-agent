package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

var statsLast int
var statsInsights bool
var statsFlow bool

var statsCmd = &cobra.Command{
	GroupID: "diagnostics",
	Use:     "stats",
	Short:   "Summarize token usage and cost from recent sessions",
	Long: `Reads .tu-agent/logs/telemetry.jsonl and prints a summary of model calls,
token usage, cost, and average latency grouped by provider.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		entries, err := stats.ReadEntries(telemetryPath(root))
		if err != nil {
			return fmt.Errorf("reading telemetry: %w", err)
		}
		if statsLast > 0 && statsLast < len(entries) {
			entries = entries[len(entries)-statsLast:]
		}

		if statsFlow {
			printFlow(stats.SummarizeFlow(entries))
			return nil
		}

		if len(entries) == 0 {
			fmt.Println("No telemetry data found in .tu-agent/logs/telemetry.jsonl")
			return nil
		}

		if statsInsights {
			printInsights(stats.SummarizeInsights(entries))
			return nil
		}

		s := stats.Summarize(entries)
		fmt.Printf("Total model calls : %d\n", s.TotalCalls)
		fmt.Printf("Total cost        : $%.4f\n\n", s.TotalCostUSD)

		providers := make([]string, 0, len(s.ByProvider))
		for p := range s.ByProvider {
			providers = append(providers, p)
		}
		sort.Strings(providers)

		for _, p := range providers {
			ps := s.ByProvider[p]
			fmt.Printf("Provider: %s\n", p)
			fmt.Printf("  Calls         : %d\n", ps.Calls)
			fmt.Printf("  Input tokens  : %d\n", ps.InputTokens)
			fmt.Printf("  Output tokens : %d\n", ps.OutputTokens)
			fmt.Printf("  Cost          : $%.4f\n", ps.TotalCostUSD)
			fmt.Printf("  Avg latency   : %.0f ms\n\n", ps.AvgLatencyMS())
		}
		return nil
	},
}

func init() {
	statsCmd.Flags().IntVar(&statsLast, "last", 0,
		"limit to last N telemetry rows (0 = all)")
	statsCmd.Flags().BoolVar(&statsInsights, "insights", false,
		"print a measurement-observability report (tool usage, staleness, hooks) instead of cost")
	statsCmd.Flags().BoolVar(&statsFlow, "flow", false,
		"print the per-feature tdd gate/review funnel instead of cost")
}

// printFlow renders the deterministic --flow diagnostic report: one line per
// tdd feature (gate attempts, failure reasons, final status) plus the
// run-level review outcome. Mirrors printInsights' fmt.Printf table style.
func printFlow(s stats.FlowSummary) {
	if len(s.Features) == 0 && s.ReviewStage == "" {
		fmt.Println("No flow events recorded yet.")
		return
	}

	names := make([]string, 0, len(s.Features))
	for name := range s.Features {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Flow funnel:")
	fmt.Printf("  %-20s %6s %6s %-30s %6s\n", "feature", "red", "green", "failures", "final")
	for _, name := range names {
		ff := s.Features[name]

		reasons := make([]string, 0, len(ff.Failures))
		for reason := range ff.Failures {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		failParts := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			failParts = append(failParts, fmt.Sprintf("%s:%d", reason, ff.Failures[reason]))
		}
		failures := "-"
		if len(failParts) > 0 {
			failures = strings.Join(failParts, " ")
		}

		final := ff.FinalStatus
		if final == "" {
			final = "-"
		}

		fmt.Printf("  %-20s %6d %6d %-30s %6s\n", name, ff.RedAttempts, ff.GreenAttempts, failures, final)
	}

	if s.ReviewStage != "" {
		fmt.Printf("Review (%s): %s\n", s.ReviewStage, s.ReviewOutcome)
	}
}

// printInsights renders the deterministic --insights diagnostic report: tool
// usage sorted by call volume, unused registered tools, mem_search zero-result
// rate, graph staleness, and hook reliability. This is a CLI-only diagnostic
// (documented §10 deviation — deterministic, no generative path) and is the
// one place the CLI joins the telemetry aggregator with the registered MCP
// tool list.
func printInsights(s stats.InsightsSummary) {
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ci, cj := s.Tools[names[i]].Calls, s.Tools[names[j]].Calls
		if ci != cj {
			return ci > cj
		}
		return names[i] < names[j]
	})

	fmt.Println("Tool usage:")
	fmt.Printf("  %-20s %6s %6s %10s %8s\n", "tool", "calls", "zero%", "avgBytes", "p50ms")
	for _, name := range names {
		ti := s.Tools[name]
		zeroPct := 0.0
		if ti.Calls > 0 {
			zeroPct = 100 * float64(ti.ZeroResults) / float64(ti.Calls)
		}
		avgBytes := 0
		if ti.Calls > 0 {
			avgBytes = ti.TotalBytes / ti.Calls
		}
		avgMS := int64(0)
		if ti.Calls > 0 {
			avgMS = ti.TotalDurMS / int64(ti.Calls)
		}
		fmt.Printf("  %-20s %6d %5.0f%% %10d %8d\n", name, ti.Calls, zeroPct, avgBytes, avgMS)
	}

	_, toolNames := buildMCPServer()
	unused := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		if _, seen := s.Tools[name]; !seen {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	fmt.Printf("\nUnused tools: %s\n", strings.Join(unused, ", "))

	if ms, ok := s.Tools["mem_search"]; ok && ms.Calls > 0 {
		fmt.Printf("\nmem_search zero-result rate: %.0f%% (%d/%d)\n",
			100*float64(ms.ZeroResults)/float64(ms.Calls), ms.ZeroResults, ms.Calls)
	}

	fmt.Println("\nGraph staleness:")
	fmt.Printf("  refreshes           : %d\n", s.GraphRefreshes)
	fmt.Printf("  failed files (total): %d\n", s.GraphFailedFiles)
	fmt.Printf("  last parsed/deleted : %d/%d\n", s.GraphLastParsed, s.GraphLastDeleted)

	fmt.Println("\nHooks:")
	fmt.Printf("  calls    : %d\n", s.HookCalls)
	fmt.Printf("  failures : %d\n", s.HookFailures)
	fmt.Printf("  p50/p95  : %dms / %dms\n",
		stats.Percentile(s.HookDurationsMS, 50), stats.Percentile(s.HookDurationsMS, 95))

	fmt.Println("\nBehavioral:")
	outcomes := make([]string, 0, len(s.Violations))
	for outcome := range s.Violations {
		outcomes = append(outcomes, outcome)
	}
	sort.Strings(outcomes)
	if len(outcomes) == 0 {
		fmt.Printf("  %-16s: none\n", "violations")
	}
	for _, outcome := range outcomes {
		fmt.Printf("  %-16s: %d\n", fmt.Sprintf("violations (%s)", outcome), s.Violations[outcome])
	}
	fmt.Printf("  %-16s: %d\n", "prompts", s.Prompts)
	fmt.Printf("  %-16s: %d\n", "prompt sessions", s.PromptSessions)
}
