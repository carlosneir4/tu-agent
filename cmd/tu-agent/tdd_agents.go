package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

var (
	tddPromptTicket  string
	tddOverlayTicket string
	tddPromptBase    string
	tddOverlayBase   string
)

var tddPromptCmd = &cobra.Command{
	Use:   "prompt <stage> [feature description...]",
	Short: "Print the composed stage prompt (agent body + overlay) for general-purpose dispatch",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		relBase := tdd.PromptRelBase(tddPromptBase, tddPromptTicket, args[1:])
		root := repoRoot()
		grounding := buildGrounding(root, args[0], relBase)
		out, err := tdd.ComposeStagePromptWithGrounding(root, args[0], relBase, grounding)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

var tddCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Report that the tdd flow can run (dev-flow roles resolve to embedded shells unless overridden)",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		// Never fatal: with the F7-B loadAgentBody fallback, a missing
		// .claude/agents/<role>.md resolves to an embedded generic shell.
		// tdd.ValidateTddAgents is kept for INFORMATION — it tells us which roles
		// resolve to a shell (no repo-level file) vs are overridden by one.
		if missing := tdd.ValidateTddAgents(root); len(missing) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "tdd flow can run — %d role(s) resolve to embedded generic shells; add .claude/agents/<role>.md to override (%s)\n", len(missing), strings.Join(missing, ", "))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "tdd flow can run — all dev-flow agents present")
		return nil
	},
}

var tddOverlayCmd = &cobra.Command{
	Use:   "overlay <stage> [feature description...]",
	Short: "Print the generic TDD contract overlay for a stage (single source for the plugin)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o, ok := tdd.StageOverlay(args[0])
		if !ok {
			return fmt.Errorf("tdd overlay: unknown stage %q", args[0])
		}
		relBase := tdd.PromptRelBase(tddOverlayBase, tddOverlayTicket, args[1:])
		fmt.Fprintln(cmd.OutOrStdout(), tdd.WithBaseDir(o, relBase))
		return nil
	},
}

var tddVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: `Run the project's resolved test command and print {"ok":bool} (trivial/refactor verification)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		passed, err := tdd.RunVerify(cmd.Context(), cfg, repoRoot())
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
