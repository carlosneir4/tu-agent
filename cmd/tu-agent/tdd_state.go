package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/tdd"
)

func tddStatePath() string {
	return filepath.Join(repoRoot(), ".tu-agent", "tdd", "state.json")
}

var tddStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the current tdd run state as JSON (features, statuses, resumable)",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := tdd.LoadState(tddStatePath())
		if err != nil {
			return fmt.Errorf("tdd status: %w", err)
		}
		out := struct {
			Task      string             `json:"task"`
			Branch    string             `json:"branch,omitempty"`
			Resumable bool               `json:"resumable"`
			Features  []tdd.FeatureState `json:"features"`
		}{st.Task, st.Branch, st.Resumable(), st.Features}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("tdd status: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
		return nil
	},
}

var (
	tddStateFeatures []string
	tddStateTask     string
	tddStateBranch   string
)

var tddStateCmd = &cobra.Command{
	Use:   "state",
	Short: "Manage the durable tdd run state (begin, mark)",
}

var tddStateBeginCmd = &cobra.Command{
	Use:   "begin",
	Short: "Start a fresh run state with the given features all pending",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(tddStateFeatures) == 0 {
			return fmt.Errorf("tdd state begin: at least one --feature is required")
		}
		st := tdd.BeginRun(tddStateTask, tddStateBranch, tddStateFeatures)
		if err := tdd.SaveState(tddStatePath(), st); err != nil {
			return fmt.Errorf("tdd state begin: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "began run with %d feature(s)\n", len(tddStateFeatures))
		return nil
	},
}

var tddStateMarkCmd = &cobra.Command{
	Use:   "mark <feature> <pass|blocked|pending>",
	Short: "Mark a feature's terminal status in the run state",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, status := args[0], args[1]
		if status != "pass" && status != "blocked" && status != "pending" {
			return fmt.Errorf("tdd state mark: status must be pass|blocked|pending, got %q", status)
		}
		st, err := tdd.LoadState(tddStatePath())
		if err != nil {
			return fmt.Errorf("tdd state mark: %w", err)
		}
		known := false
		for _, f := range st.Features {
			if f.Name == name {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("tdd state mark: unknown feature %q", name)
		}
		st.Mark(name, status)
		if err := tdd.SaveState(tddStatePath(), st); err != nil {
			return fmt.Errorf("tdd state mark: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "marked %s %s\n", name, status)
		return nil
	},
}

func init() {
	tddStateBeginCmd.Flags().StringArrayVar(&tddStateFeatures, "feature", nil, "feature slug (repeatable)")
	tddStateBeginCmd.Flags().StringVar(&tddStateTask, "task", "", "the run's task description")
	tddStateBeginCmd.Flags().StringVar(&tddStateBranch, "branch", "", "the feature branch")
	tddStateCmd.AddCommand(tddStateBeginCmd)
	tddStateCmd.AddCommand(tddStateMarkCmd)
	tddCmd.AddCommand(tddStatusCmd)
	tddCmd.AddCommand(tddStateCmd)
}
