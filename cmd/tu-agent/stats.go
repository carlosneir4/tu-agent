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

var statsCmd = &cobra.Command{
	GroupID: "diagnostics",
	Use:     "stats",
	Short:   "Summarize token usage and cost from recent sessions",
	Long: `Reads .tu-agent/telemetry.jsonl and prints a summary of model calls,
token usage, cost, and average latency grouped by provider.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := stats.ReadEntries(".tu-agent/telemetry.jsonl")
		if err != nil {
			return fmt.Errorf("reading telemetry: %w", err)
		}
		if len(entries) == 0 {
			fmt.Println("No telemetry data found in .tu-agent/telemetry.jsonl")
			return nil
		}
		if statsLast > 0 && statsLast < len(entries) {
			entries = entries[len(entries)-statsLast:]
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
		"limit to last N model calls (0 = all)")
	statsCmd.Flags().BoolVar(&statsInsights, "insights", false,
		"print a measurement-observability report (tool usage, staleness, hooks) instead of cost")
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
