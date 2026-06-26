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
	default:
		return "", false
	}
}

// composeStagePrompt builds the general-purpose dispatch prompt for a stage:
// the project's agent body (role knowledge) joined with the generic TDD overlay.
// It is exactly what the CLI conductor composes in tddStageDefs, exposed so the
// plugin can dispatch general-purpose without depending on agent registration.
func composeStagePrompt(root, stage string) (string, error) {
	for _, st := range tddStages() {
		if st.stage == stage {
			body, err := loadAgentBody(root, st.role)
			if err != nil {
				return "", fmt.Errorf("tdd prompt: %w — run `tu-agent init`", err)
			}
			return body + "\n\n" + st.overlay, nil
		}
	}
	return "", fmt.Errorf("tdd prompt: unknown stage %q", stage)
}

var tddPromptCmd = &cobra.Command{
	Use:   "prompt <analyst|architect|craftsman|judge|scribe>",
	Short: "Print the composed stage prompt (agent body + overlay) for general-purpose dispatch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := composeStagePrompt(repoRoot(), args[0])
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
	Use:   "overlay <analyst|architect|craftsman|judge|scribe>",
	Short: "Print the generic TDD contract overlay for a stage (single source for the plugin)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o, ok := tddOverlay(args[0])
		if !ok {
			return fmt.Errorf("tdd overlay: unknown stage %q", args[0])
		}
		fmt.Fprintln(cmd.OutOrStdout(), o)
		return nil
	},
}

func init() {
	tddCmd.AddCommand(tddCheckCmd)
	tddCmd.AddCommand(tddOverlayCmd)
	tddCmd.AddCommand(tddPromptCmd)
}
