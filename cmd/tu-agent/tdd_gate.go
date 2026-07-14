package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

var (
	tddGateFeature  string
	tddGateCovered  string
	tddGateExpect   string
	tddGateNewTests string
	tddGateTicket   string
	tddGateBase     string
)

var tddGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Run the deterministic gate (green tests + @s coverage) and print JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := tdd.RunGate(cmd.Context(), cfg, repoRoot(), tddGateTicket, tddGateFeature, tddGateCovered, tddGateExpect, tddGateNewTests, tddGateBase, tdd.ResolveTestRunner)
		if err != nil {
			return fmt.Errorf("tdd gate: %w", err)
		}
		out, err := json.Marshal(res)
		if err != nil {
			return fmt.Errorf("tdd gate: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	tddGateCmd.Flags().StringVar(&tddGateFeature, "feature", "", "feature name (reads <base>/features/<name>.feature)")
	tddGateCmd.Flags().StringVar(&tddGateCovered, "covered", "", "comma-separated @s tags the craftsman covered")
	tddGateCmd.Flags().StringVar(&tddGateExpect, "expect", "green", "expected color: green | red")
	tddGateCmd.Flags().StringVar(&tddGateNewTests, "new-tests", "", "comma-separated new test file paths (for --expect red)")
	tddGateCmd.Flags().StringVar(&tddGateTicket, "ticket", "", "ticket id to address a specific run's feature dir")
	tddGateCmd.Flags().StringVar(&tddGateBase, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddCmd.AddCommand(tddGateCmd)
}
