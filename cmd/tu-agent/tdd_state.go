package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/tdd"
)

var (
	tddStateTicket   string
	tddStateBaseFlag string
)

// tddStatePath resolves the state.json for the addressed run: by ticket, else
// the newest run, else a legacy flat run. Falls back to the flat path so a
// fresh `state begin` has somewhere to write.
func tddStatePath(root, ticket string) string {
	if base, ok := resolveTddBase(root, ticket); ok {
		return filepath.Join(base, "state.json")
	}
	return filepath.Join(root, ".tu-agent", "tdd", "state.json")
}

// tddStateFile resolves the state.json for state/status commands: an explicit
// --base wins (relative is joined to root); otherwise falls back to
// --ticket/mtime resolution via tddStatePath.
func tddStateFile(root string) string {
	if tddStateBaseFlag != "" {
		b := tddStateBaseFlag
		if !filepath.IsAbs(b) {
			b = filepath.Join(root, filepath.FromSlash(b))
		}
		return filepath.Join(b, "state.json")
	}
	return tddStatePath(root, tddStateTicket)
}

// tddStateBaseRel is the repo-relative dir that holds the resolved state.json.
func tddStateBaseRel(root, statePath string) string {
	rel, err := filepath.Rel(root, filepath.Dir(statePath))
	if err != nil {
		return ""
	}
	return rel
}

var tddStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the current tdd run state as JSON (features, statuses, resumable)",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		sp := tddStateFile(root)
		st, err := tdd.LoadState(sp)
		if err != nil {
			return fmt.Errorf("tdd status: %w", err)
		}
		out := struct {
			Task      string             `json:"task"`
			Branch    string             `json:"branch,omitempty"`
			Base      string             `json:"base"`
			Resumable bool               `json:"resumable"`
			Features  []tdd.FeatureState `json:"features"`
		}{st.Task, st.Branch, tddStateBaseRel(root, sp), st.Resumable(), st.Features}
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
		feats := make([]tdd.FeaturePlan, 0, len(tddStateFeatures))
		for _, name := range tddStateFeatures {
			feats = append(feats, tdd.FeaturePlan{Name: name})
		}
		st, err := tdd.BeginRun(tddStateTask, tddStateBranch, feats)
		if err != nil {
			return fmt.Errorf("tdd state begin: %w", err)
		}
		if err := tdd.SaveState(tddStateFile(repoRoot()), st); err != nil {
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
		st, err := tdd.LoadState(tddStateFile(repoRoot()))
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
		if err := tdd.SaveState(tddStateFile(repoRoot()), st); err != nil {
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
	tddStateBeginCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateBeginCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateMarkCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateMarkCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStatusCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStatusCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateCmd.AddCommand(tddStateBeginCmd)
	tddStateCmd.AddCommand(tddStateMarkCmd)
	tddCmd.AddCommand(tddStatusCmd)
	tddCmd.AddCommand(tddStateCmd)
}
