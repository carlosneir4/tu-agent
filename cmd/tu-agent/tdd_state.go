package main

import (
	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

var (
	tddStateTicket   string
	tddStateBaseFlag string
)

var tddStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the current tdd run state as JSON (features, statuses, resumable)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tdd.RunStatus(repoRoot(), tddStateBaseFlag, tddStateTicket, cmd.OutOrStdout())
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
		return tdd.RunStateBegin(repoRoot(), tddStateBaseFlag, tddStateTicket, tddStateTask, tddStateBranch, tddStateFeatures, cmd.OutOrStdout())
	},
}

var tddStateMarkCmd = &cobra.Command{
	Use:   "mark <feature> <pass|blocked|pending>",
	Short: "Mark a feature's terminal status in the run state",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tdd.RunStateMark(repoRoot(), tddStateBaseFlag, tddStateTicket, args[0], args[1], cmd.OutOrStdout())
	},
}

var tddStateReviewCmd = &cobra.Command{
	Use:   "review <pending|pass|skipped>",
	Short: "Set the design review gate status in the run state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tdd.RunStateReview(repoRoot(), tddStateBaseFlag, tddStateTicket, args[0], cmd.OutOrStdout())
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
	tddStateReviewCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateReviewCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStatusCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStatusCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateCmd.AddCommand(tddStateBeginCmd)
	tddStateCmd.AddCommand(tddStateMarkCmd)
	tddStateCmd.AddCommand(tddStateReviewCmd)
	tddCmd.AddCommand(tddStatusCmd)
	tddCmd.AddCommand(tddStateCmd)
}
