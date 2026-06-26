package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/stats"
)

var statsLast int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Summarize token usage and cost from recent sessions",
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
}
