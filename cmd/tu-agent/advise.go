package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/advise"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/stats"
)

// adviseStateKey is the memory-store metadata key advise's dedup/dismiss
// state (a JSON-encoded advise.State) is persisted under.
const adviseStateKey = "advise_state"

// adviseNudgeBudget caps how many suggestions `advise --nudge` prints (and
// persists as shown) per SessionStart run.
const adviseNudgeBudget = 2

var adviseNudge bool

// adviseCmd is the deterministic §10 CLI surface for advise: it evaluates
// telemetry insights and crystallize state against internal/advise's rules
// and prints evidence-bearing suggestions. Plain mode is a full diagnostic
// (no dedup, no persistence); --nudge is the deduped, budgeted channel meant
// for the SessionStart hook (see plugin/hooks/hooks.json and
// internal/codegen/harden.go).
var adviseCmd = &cobra.Command{
	GroupID: "memory",
	Use:     "advise",
	Short:   "Deterministic, evidence-based suggestions from telemetry and memory state",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if adviseNudge {
			start := time.Now()
			err := runAdviseNudge(cmd)
			recordHook("advise", time.Since(start), err)
			// Hooks must never fail the session: degrade quietly regardless
			// of what went wrong above (already recorded to telemetry).
			return nil
		}
		return runAdvisePlain(cmd)
	},
}

// adviseDismissCmd permanently suppresses one rule's suggestions.
var adviseDismissCmd = &cobra.Command{
	Use:   "dismiss <rule-id>",
	Short: "Permanently suppress a rule's advise suggestions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAdviseDismiss(cmd, args[0])
	},
}

// adviseResetCmd clears all persisted advise dedup/dismiss state.
var adviseResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear all persisted advise dedup/dismiss state",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAdviseReset(cmd)
	},
}

func init() {
	adviseCmd.Flags().BoolVar(&adviseNudge, "nudge", false,
		"print at most 2 new-evidence suggestions and persist dedup state (for hooks)")
	adviseCmd.AddCommand(adviseDismissCmd)
	adviseCmd.AddCommand(adviseResetCmd)
	rootCmd.AddCommand(adviseCmd)
}

// knownAdviseRules are the stable RuleIDs internal/advise.Evaluate can
// produce — the only ids `advise dismiss` accepts.
var knownAdviseRules = map[string]bool{
	"crystallize-ready":    true,
	"learn-stale":          true,
	"edit-without-context": true,
	"secret-guard":         true,
	"mem-search-zero":      true,
}

// adviseInputs gathers advise.Inputs from telemetry and crystallize state
// under root. A missing telemetry.jsonl (fresh repo, or telemetry disabled)
// tolerates to empty insights, matching stats.ReadEntries.
func adviseInputs(root string) (advise.Inputs, error) {
	entries, err := stats.ReadEntries(telemetryPath(root))
	if err != nil {
		return advise.Inputs{}, fmt.Errorf("adviseInputs: %w", err)
	}
	needs, err := crystallizeNeeds(root)
	if err != nil {
		return advise.Inputs{}, fmt.Errorf("adviseInputs: %w", err)
	}
	uncovered, err := uncoveredFileCount(root)
	if err != nil {
		return advise.Inputs{}, fmt.Errorf("adviseInputs: %w", err)
	}
	return advise.Inputs{
		Insights:         stats.SummarizeInsights(entries),
		CrystallizeNeeds: needs,
		UncoveredFiles:   uncovered,
	}, nil
}

// loadAdviseState reads and parses the persisted advise state from the
// memory store's metadata table. A store with no state yet parses to an
// empty State (see advise.ParseState).
func loadAdviseState(root string) (advise.State, error) {
	s, err := memory.Open(memoryDBPath(root))
	if err != nil {
		return advise.State{}, fmt.Errorf("loadAdviseState: %w", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	raw, err := s.Meta(adviseStateKey)
	if err != nil {
		return advise.State{}, fmt.Errorf("loadAdviseState: %w", err)
	}
	st, err := advise.ParseState(raw)
	if err != nil {
		return advise.State{}, fmt.Errorf("loadAdviseState: %w", err)
	}
	return st, nil
}

// saveAdviseState persists advise state to the memory store's metadata
// table.
func saveAdviseState(root string, st advise.State) error {
	raw, err := st.Marshal()
	if err != nil {
		return fmt.Errorf("saveAdviseState: %w", err)
	}
	s, err := memory.Open(memoryDBPath(root))
	if err != nil {
		return fmt.Errorf("saveAdviseState: %w", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	if err := s.SetMeta(adviseStateKey, raw); err != nil {
		return fmt.Errorf("saveAdviseState: %w", err)
	}
	return nil
}

// runAdvisePlain is the full diagnostic: every threshold-meeting suggestion
// not dismissed, one per line, in Evaluate's deterministic order. No dedup,
// no persistence.
func runAdvisePlain(cmd *cobra.Command) error {
	root := repoRoot()
	in, err := adviseInputs(root)
	if err != nil {
		return err
	}
	st, err := loadAdviseState(root)
	if err != nil {
		return err
	}
	for _, s := range advise.Evaluate(in) {
		if st.Rules[s.RuleID].Dismissed {
			continue
		}
		fmt.Fprintln(cmd.OutOrStdout(), s.Message)
	}
	return nil
}

// runAdviseNudge is the SessionStart hook channel: dedup by evidence-growth,
// cap at adviseNudgeBudget, persist the resulting state.
func runAdviseNudge(cmd *cobra.Command) error {
	root := repoRoot()
	in, err := adviseInputs(root)
	if err != nil {
		return err
	}
	st, err := loadAdviseState(root)
	if err != nil {
		return err
	}
	show, next := advise.Filter(advise.Evaluate(in), st, adviseNudgeBudget)
	if len(show) > 0 {
		lines := make([]string, 0, len(show))
		for _, s := range show {
			lines = append(lines, "tu-agent: "+s.Message)
		}
		// The suggestion is user-facing (an action to run), so it is both the
		// visible systemMessage and the model's additionalContext.
		msg := strings.Join(lines, "\n")
		if err := writeSessionStartHook(cmd.OutOrStdout(), msg, msg); err != nil {
			return err
		}
	}
	return saveAdviseState(root, next)
}

// runAdviseDismiss marks ruleID dismissed in the persisted state, rejecting
// unknown rule ids.
func runAdviseDismiss(cmd *cobra.Command, ruleID string) error {
	if !knownAdviseRules[ruleID] {
		return fmt.Errorf("advise dismiss: unknown rule %q", ruleID)
	}
	root := repoRoot()
	st, err := loadAdviseState(root)
	if err != nil {
		return err
	}
	rs := st.Rules[ruleID]
	rs.Dismissed = true
	st.Rules[ruleID] = rs
	if err := saveAdviseState(root, st); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "dismissed %s\n", ruleID)
	return nil
}

// runAdviseReset clears all persisted advise dedup/dismiss state.
func runAdviseReset(cmd *cobra.Command) error {
	root := repoRoot()
	if err := saveAdviseState(root, advise.State{Rules: map[string]advise.RuleState{}}); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "advise state reset")
	return nil
}
