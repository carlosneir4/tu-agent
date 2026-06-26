package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/tdd"
)

var (
	tddGateFeature string
	tddGateCovered string
)

// tddGateResult is the JSON the gate prints for the plugin skill to read.
type tddGateResult struct {
	OK       bool   `json:"ok"`
	Feedback string `json:"feedback,omitempty"`
}

// splitTags splits a comma-separated tag list, trimming spaces and dropping empties.
func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// runGate reads the feature's required @s tags, runs the deterministic gate
// (coverage + green tests) via the same functions the CLI conductor uses, and
// returns the structured result. A missing feature file or unresolved test
// command is an error (the caller distinguishes "ran and failed" from "could
// not run").
func runGate(ctx context.Context, cfg config.Config, root, feature, coveredRaw string) (tddGateResult, error) {
	if feature == "" {
		return tddGateResult{}, fmt.Errorf("--feature is required")
	}
	featPath := filepath.Join(root, ".tu-agent", "tdd", "features", feature+".feature")
	data, err := os.ReadFile(featPath)
	if err != nil {
		return tddGateResult{}, fmt.Errorf("reading feature: %w", err)
	}
	required := tdd.ScenarioTags(string(data))
	runner, err := resolveTestRunner(cfg, root)
	if err != nil {
		return tddGateResult{}, err
	}
	det := tdd.DeterministicJudge(ctx, runner, required, splitTags(coveredRaw))
	return tddGateResult{OK: det.OK, Feedback: det.Feedback}, nil
}

var tddGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Run the deterministic gate (green tests + @s coverage) and print JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runGate(cmd.Context(), cfg, repoRoot(), tddGateFeature, tddGateCovered)
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
	tddGateCmd.Flags().StringVar(&tddGateFeature, "feature", "", "feature name (reads .tu-agent/tdd/features/<name>.feature)")
	tddGateCmd.Flags().StringVar(&tddGateCovered, "covered", "", "comma-separated @s tags the craftsman covered")
	tddCmd.AddCommand(tddGateCmd)
}
