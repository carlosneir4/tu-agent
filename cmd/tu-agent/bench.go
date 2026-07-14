package main

import (
	"fmt"

	"github.com/carlosneir4/tu-agent/internal/bench"
	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/spf13/cobra"
)

var (
	benchBaseline string
	benchCompare  string
)

var benchCmd = &cobra.Command{
	Use:        "bench",
	Short:      "Compare two telemetry files and report token and cost differences",
	Deprecated: "the standalone provider harness is frozen; use tu-agent through the Claude Code plugin (see CLAUDE.md §10)",
	Long: `Reads two telemetry JSONL files (baseline and candidate) and prints a comparison
report showing token usage and cost savings. Use after running the same tasks with
different providers to measure routing effectiveness (Claim B).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if benchBaseline == "" || benchCompare == "" {
			return fmt.Errorf("--baseline and --compare are both required")
		}
		baseline, err := stats.ReadEntries(benchBaseline)
		if err != nil {
			return fmt.Errorf("reading baseline: %w", err)
		}
		candidate, err := stats.ReadEntries(benchCompare)
		if err != nil {
			return fmt.Errorf("reading candidate: %w", err)
		}

		r := bench.Compare(baseline, candidate)

		fmt.Println("=== Benchmark Comparison ===")
		fmt.Printf("\nBaseline  (%s)\n", benchBaseline)
		fmt.Printf("  Calls         : %d\n", r.BaselineCalls)
		fmt.Printf("  Total tokens  : %d\n", r.BaselineTokens)
		fmt.Printf("  Cost          : $%.4f\n", r.BaselineCostUSD)
		fmt.Printf("\nCandidate (%s)\n", benchCompare)
		fmt.Printf("  Calls         : %d\n", r.CompareCalls)
		fmt.Printf("  Total tokens  : %d\n", r.CompareTokens)
		fmt.Printf("  Cost          : $%.4f\n", r.CompareCostUSD)
		fmt.Printf("\nDelta\n")
		fmt.Printf("  Token change  : %+.1f%%\n", r.TokenDeltaPct)
		fmt.Printf("  Cost change   : %+.1f%%\n", r.CostDeltaPct)

		return nil
	},
}

func init() {
	benchCmd.Flags().StringVar(&benchBaseline, "baseline", "", "baseline telemetry JSONL file (required)")
	benchCmd.Flags().StringVar(&benchCompare, "compare", "", "candidate telemetry JSONL file (required)")
}
