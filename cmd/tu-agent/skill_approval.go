package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// skillApprovalsKey is the memory-store metadata key the foreign-skill
// approval map (name -> approved revision) is persisted under. Metadata rows
// never ride the team chunk (Store.ExportRecords reads only the observations
// table), so an approval is local-only by construction, not by filtering.
const skillApprovalsKey = "skill_approvals"

// loadSkillApprovals reads and parses the skill_approvals metadata blob
// (skill name -> approved revision) from an already-open store. Absent state
// parses to an empty map.
func loadSkillApprovals(s *memory.Store) (map[string]int, error) {
	raw, err := s.Meta(skillApprovalsKey)
	if err != nil {
		return nil, fmt.Errorf("loadSkillApprovals: %w", err)
	}
	approvals := map[string]int{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &approvals); err != nil {
			return nil, fmt.Errorf("loadSkillApprovals: parse %s: %w", skillApprovalsKey, err)
		}
	}
	return approvals, nil
}

// saveSkillApprovals persists the skill_approvals metadata blob to an
// already-open store.
func saveSkillApprovals(s *memory.Store, approvals map[string]int) error {
	raw, err := json.Marshal(approvals)
	if err != nil {
		return fmt.Errorf("saveSkillApprovals: %w", err)
	}
	if err := s.SetMeta(skillApprovalsKey, string(raw)); err != nil {
		return fmt.Errorf("saveSkillApprovals: %w", err)
	}
	return nil
}

// pendingForeignSkills returns the type=skill observations that are foreign
// (not authored by localIdentity, per isLocalAuthor) and not currently
// approved at their exact revision: a record approved at an older revision
// (re-imported with new content) counts as pending again, matching
// ImportRecords' higher-revision-wins semantics.
func pendingForeignSkills(obs []memory.Observation, approvals map[string]int, localIdentity string) []memory.Observation {
	pending := make([]memory.Observation, 0, len(obs))
	for _, o := range obs {
		if o.Type != "skill" || isLocalAuthor(o, localIdentity) {
			continue
		}
		name := strings.TrimPrefix(o.TopicKey, "skill/")
		if approvals[name] == o.Revision {
			continue
		}
		pending = append(pending, o)
	}
	return pending
}

// printPendingForeignSkills appends, to out, a section listing unapproved
// foreign type=skill records (name, author, revision) — nothing is printed
// when there are none, so it never disturbs memoryPendingCmd's existing
// chunk-diff output on the no-skills path.
func printPendingForeignSkills(out io.Writer, root, localIdentity string) error {
	return withMemStore(root, func(s *memory.Store) error {
		obs, err := s.List()
		if err != nil {
			return fmt.Errorf("memory pending: %w", err)
		}
		approvals, err := loadSkillApprovals(s)
		if err != nil {
			return fmt.Errorf("memory pending: %w", err)
		}
		pending := pendingForeignSkills(obs, approvals, localIdentity)
		if len(pending) == 0 {
			return nil
		}
		fmt.Fprintf(out, "\n%d unapproved foreign skill record(s) — review with `tu-agent memory pending`, approve with `tu-agent memory approve-skill <name>`:\n", len(pending))
		for _, o := range pending {
			name := strings.TrimPrefix(o.TopicKey, "skill/")
			fmt.Fprintf(out, "- %s (author %s, revision %d)\n", name, o.Author, o.Revision)
		}
		return nil
	})
}

// memoryApproveSkillCmd approves one foreign skill record by name, recording
// its current revision in skill_approvals and materializing it immediately.
// Deliberately CLI-only, with no MCP tool: an in-session agent must not
// self-approve a teammate's skill.
var memoryApproveSkillCmd = &cobra.Command{
	Use:   "approve-skill <name>",
	Short: "Approve a foreign skill record and materialize it (local-only; no MCP surface by design)",
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("memory approve-skill: requires exactly one skill name argument")
		}
		if invalidSkillName(args[0]) {
			return fmt.Errorf("memory approve-skill: invalid skill name %q", args[0])
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMemoryApproveSkill(cmd, args[0])
	},
}

// runMemoryApproveSkill finds the unapproved foreign type=skill record named
// name, records its current revision as approved, and materializes it right
// away (still honoring crystallize.MaterializeDecision, so a hand-edited
// SKILL.md file is never clobbered).
func runMemoryApproveSkill(cmd *cobra.Command, name string) error {
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		localIdentity := gitAuthor()
		obs, err := s.List()
		if err != nil {
			return fmt.Errorf("memory approve-skill: %w", err)
		}
		var record memory.Observation
		found := false
		for _, o := range obs {
			if o.Type == "skill" && strings.TrimPrefix(o.TopicKey, "skill/") == name && !isLocalAuthor(o, localIdentity) {
				record = o
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("memory approve-skill: no unapproved foreign skill record named %q", name)
		}
		approvals, err := loadSkillApprovals(s)
		if err != nil {
			return fmt.Errorf("memory approve-skill: %w", err)
		}
		approvals[name] = record.Revision
		if err := saveSkillApprovals(s, approvals); err != nil {
			return fmt.Errorf("memory approve-skill: %w", err)
		}
		if _, err := materializeSkillFile(generatedSkillsDir(repoRoot()), name, record.Content); err != nil {
			return fmt.Errorf("memory approve-skill: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "approved %s at revision %d\n", name, record.Revision)
		return nil
	})
}
