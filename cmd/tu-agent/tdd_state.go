package main

import (
	"fmt"

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
	tddStateFeatures   []string
	tddStateTask       string
	tddStateBranch     string
	tddStateComplexity string
)

var tddStateCmd = &cobra.Command{
	Use:   "state",
	Short: "Manage the durable tdd run state (begin, mark)",
}

var tddStateBeginCmd = &cobra.Command{
	Use:   "begin",
	Short: "Start a fresh run state with the given features all pending",
	RunE: func(cmd *cobra.Command, args []string) error {
		// complexity is captured and the flag var reset immediately so a
		// value set on one invocation (tests drive RunE directly, reusing
		// the same package-level flag var) never leaks into the next.
		complexity := tddStateComplexity
		tddStateComplexity = ""
		if err := tdd.RunStateBegin(repoRoot(), tddStateBaseFlag, tddStateTicket, tddStateTask, tddStateBranch, tddStateFeatures, complexity, cmd.OutOrStdout()); err != nil {
			return err
		}
		recordTddStage("begin", "", "begin")
		return nil
	},
}

var tddApproveRounds int

var tddStateApproveDesignCmd = &cobra.Command{
	Use:   "approve-design",
	Short: "Record a durable design-approval token and stamp plan.md's Sign-off section",
	RunE: func(cmd *cobra.Command, args []string) error {
		// rounds is captured and the flag var reset immediately, same
		// gotcha as tddStateComplexity above.
		rounds := tddApproveRounds
		tddApproveRounds = 1
		approver := gitAuthor()
		if approver == "" {
			return fmt.Errorf("tdd state approve-design: git user.email is not set; run `git config user.email you@example.com` before approving a design")
		}
		if err := tdd.RunStateApproveDesign(repoRoot(), tddStateBaseFlag, tddStateTicket, rounds, approver, cmd.OutOrStdout()); err != nil {
			return err
		}
		recordTddStage("approve-design", "", fmt.Sprintf("rounds:%d", rounds))
		return nil
	},
}

var tddStateMarkCmd = &cobra.Command{
	Use:   "mark <feature> <pass|blocked|pending>",
	Short: "Mark a feature's terminal status in the run state",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := tdd.RunStateMark(repoRoot(), tddStateBaseFlag, tddStateTicket, args[0], args[1], cmd.OutOrStdout()); err != nil {
			return err
		}
		recordTddStage("mark", args[0], args[1])
		return nil
	},
}

var tddStateReviewFindings string

var tddStateReviewCmd = &cobra.Command{
	Use:   "review <pending|pass|skipped>",
	Short: "Set the design review gate status in the run state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// findings is captured and the flag var reset immediately so a value
		// set on one invocation (tests drive the RunE directly, reusing the
		// same package-level flag var) never leaks into the next.
		findings := tddStateReviewFindings
		tddStateReviewFindings = ""
		if err := tdd.RunStateReview(repoRoot(), tddStateBaseFlag, tddStateTicket, args[0], cmd.OutOrStdout()); err != nil {
			return err
		}
		if findings != "" {
			recordTddStage("branch-review", "", findings)
		} else {
			recordTddStage("review", "", args[0])
		}
		return nil
	},
}

func init() {
	tddStateBeginCmd.Flags().StringArrayVar(&tddStateFeatures, "feature", nil, "feature slug (repeatable)")
	tddStateBeginCmd.Flags().StringVar(&tddStateTask, "task", "", "the run's task description")
	tddStateBeginCmd.Flags().StringVar(&tddStateBranch, "branch", "", "the feature branch")
	tddStateBeginCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateBeginCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateBeginCmd.Flags().StringVar(&tddStateComplexity, "complexity", "", "run complexity: trivial|standard|complex (standard/complex require a prior `tdd state approve-design`)")
	tddStateApproveDesignCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateApproveDesignCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateApproveDesignCmd.Flags().IntVar(&tddApproveRounds, "rounds", 1, "number of design review rounds consumed before this approval")
	tddStateMarkCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateMarkCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateReviewCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStateReviewCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateReviewCmd.Flags().StringVar(&tddStateReviewFindings, "findings", "", "findings code for a whole-branch review outcome (e.g. critical:1,important:2)")
	tddStatusCmd.Flags().StringVar(&tddStateTicket, "ticket", "", "ticket id to address a specific run")
	tddStatusCmd.Flags().StringVar(&tddStateBaseFlag, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddStateCmd.AddCommand(tddStateBeginCmd)
	tddStateCmd.AddCommand(tddStateApproveDesignCmd)
	tddStateCmd.AddCommand(tddStateMarkCmd)
	tddStateCmd.AddCommand(tddStateReviewCmd)
	tddCmd.AddCommand(tddStatusCmd)
	tddCmd.AddCommand(tddStateCmd)
}
