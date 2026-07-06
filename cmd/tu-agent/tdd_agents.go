package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/tdd"
)

// tddOverlay returns the generic TDD contract overlay for a stage. It is the
// single source the plugin conductor fetches (via `tu-agent tdd overlay`) and
// the CLI references through the consts directly.
func tddOverlay(stage string) (string, bool) {
	switch stage {
	case "analyst":
		return tdd.AnalystPrompt, true
	case "architect":
		return tdd.ArchitectPrompt, true
	case "craftsman":
		return tdd.CraftsmanPrompt, true
	case "judge":
		return tdd.JudgePrompt, true
	case "scribe":
		return tdd.ScribePrompt, true
	case "test-writer":
		return tdd.TestWriterPrompt, true
	case "implementer":
		return tdd.ImplementerPrompt, true
	default:
		return "", false
	}
}

// composeStagePrompt builds the general-purpose dispatch prompt for a stage:
// the project's agent body (role knowledge) joined with the generic TDD overlay.
// It is exactly what the CLI conductor composes in tddStageDefs, exposed so the
// plugin can dispatch general-purpose without depending on agent registration.
func composeStagePrompt(root, stage, relBase string) (string, error) {
	for _, st := range tddStages() {
		if st.stage == stage {
			body, err := loadAgentBody(root, st.role)
			if err != nil {
				return "", fmt.Errorf("tdd prompt: %w — run `tu-agent init`", err)
			}
			return body + "\n\n" + tdd.WithBaseDir(st.overlay, relBase), nil
		}
	}
	return "", fmt.Errorf("tdd prompt: unknown stage %q", stage)
}

var (
	tddPromptTicket  string
	tddOverlayTicket string
	tddPromptBase    string
	tddOverlayBase   string
)

// promptRelBase picks the per-feature base dir for a stage prompt: an explicit
// --base wins (used by the plugin, which resolves $BASE once); otherwise it is
// derived from the ticket + feature description.
func promptRelBase(base, ticket string, descArgs []string) string {
	if base != "" {
		return base
	}
	return tddRelBase(ticket, slugify(strings.Join(descArgs, " ")))
}

var tddPromptCmd = &cobra.Command{
	Use:   "prompt <stage> [feature description...]",
	Short: "Print the composed stage prompt (agent body + overlay) for general-purpose dispatch",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		relBase := promptRelBase(tddPromptBase, tddPromptTicket, args[1:])
		out, err := composeStagePrompt(repoRoot(), args[0], relBase)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

var tddCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify the dev-flow agents the tdd flow needs exist (run tu-agent init if not)",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		if missing := validateTddAgents(root); len(missing) > 0 {
			return fmt.Errorf("missing dev-flow agents in .claude/agents/ (%s) — run `tu-agent init`", strings.Join(missing, ", "))
		}
		fmt.Fprintln(cmd.OutOrStdout(), "all dev-flow agents present")
		return nil
	},
}

var tddOverlayCmd = &cobra.Command{
	Use:   "overlay <stage> [feature description...]",
	Short: "Print the generic TDD contract overlay for a stage (single source for the plugin)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o, ok := tddOverlay(args[0])
		if !ok {
			return fmt.Errorf("tdd overlay: unknown stage %q", args[0])
		}
		relBase := promptRelBase(tddOverlayBase, tddOverlayTicket, args[1:])
		fmt.Fprintln(cmd.OutOrStdout(), tdd.WithBaseDir(o, relBase))
		return nil
	},
}

func init() {
	tddCmd.AddCommand(tddCheckCmd)
	tddCmd.AddCommand(tddOverlayCmd)
	tddCmd.AddCommand(tddPromptCmd)
	tddPromptCmd.Flags().StringVar(&tddPromptTicket, "ticket", "", "ticket id for the per-feature artifact dir")
	tddOverlayCmd.Flags().StringVar(&tddOverlayTicket, "ticket", "", "ticket id for the per-feature artifact dir")
	tddPromptCmd.Flags().StringVar(&tddPromptBase, "base", "", "explicit per-feature base dir (overrides --ticket/desc)")
	tddOverlayCmd.Flags().StringVar(&tddOverlayBase, "base", "", "explicit per-feature base dir (overrides --ticket/desc)")
}
