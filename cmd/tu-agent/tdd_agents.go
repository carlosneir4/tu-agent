package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
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
	case "refactor":
		return tdd.RefactorPrompt, true
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

// runVerify runs the resolved test command and reports whether it passed. It
// returns an error only when the runner itself could not run (misconfigured
// or missing test command) — a failing test suite is a normal (false, nil)
// result, not an error, so `tdd verify` can print {"ok":false} with exit 0.
func runVerify(ctx context.Context, cfg config.Config, root string) (bool, error) {
	runner, err := resolveTestRunner(cfg, root)
	if err != nil {
		return false, fmt.Errorf("runVerify: %w", err)
	}
	passed, _, err := runner(ctx)
	if err != nil {
		return false, fmt.Errorf("runVerify: %w", err)
	}
	return passed, nil
}

var tddVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: `Run the project's resolved test command and print {"ok":bool} (trivial/refactor verification)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		passed, err := runVerify(cmd.Context(), cfg, repoRoot())
		if err != nil {
			return fmt.Errorf("tdd verify: %w", err)
		}
		out, err := json.Marshal(struct {
			OK bool `json:"ok"`
		}{passed})
		if err != nil {
			return fmt.Errorf("tdd verify: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	tddCmd.AddCommand(tddCheckCmd)
	tddCmd.AddCommand(tddOverlayCmd)
	tddCmd.AddCommand(tddPromptCmd)
	tddCmd.AddCommand(tddVerifyCmd)
	tddPromptCmd.Flags().StringVar(&tddPromptTicket, "ticket", "", "ticket id for the per-feature artifact dir")
	tddOverlayCmd.Flags().StringVar(&tddOverlayTicket, "ticket", "", "ticket id for the per-feature artifact dir")
	tddPromptCmd.Flags().StringVar(&tddPromptBase, "base", "", "explicit per-feature base dir (overrides --ticket/desc)")
	tddOverlayCmd.Flags().StringVar(&tddOverlayBase, "base", "", "explicit per-feature base dir (overrides --ticket/desc)")
}
