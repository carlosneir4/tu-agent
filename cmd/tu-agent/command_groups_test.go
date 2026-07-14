package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// These tests pin the CONFIRMED help taxonomy: five cobra command groups on
// rootCmd (stable IDs "setup","graph","memory","feature","diagnostics"), the
// GroupID assignment per command, and Hidden flags on the two hook-only memory
// subcommands. They are written RED-first against the current (ungrouped) tree.

// @s1: the five taxonomy groups exist on rootCmd.
func TestRootCommandGroupsExist(t *testing.T) {
	wantIDs := []string{"setup", "graph", "memory", "feature", "diagnostics"}

	groups := rootCmd.Groups()
	have := make(map[string]bool, len(groups))
	for _, g := range groups {
		if g == nil {
			continue
		}
		have[g.ID] = true
	}

	for _, id := range wantIDs {
		if !have[id] {
			t.Errorf("rootCmd.Groups() is missing a group with ID %q; have %v", id, groupIDs(groups))
		}
	}
}

// @s2: each grouped root command carries the expected GroupID.
func TestCommandGroupIDAssignments(t *testing.T) {
	cases := []struct {
		name    string
		cmd     *cobra.Command
		groupID string
	}{
		{"initCmd", initCmd, "setup"},

		{"graphCmd", graphCmd, "graph"},
		{"learnCmd", learnCmd, "graph"},
		{"mapCmd", mapCmd, "graph"},
		{"conceptsCmd", conceptsCmd, "graph"},

		{"memoryCmd", memoryCmd, "memory"},
		{"adviseCmd", adviseCmd, "memory"},
		{"sessionCmd", sessionCmd, "memory"},

		{"tddCmd", tddCmd, "feature"},
		{"testCmd", testCmd, "feature"},

		{"statsCmd", statsCmd, "diagnostics"},
		{"topStatusCmd", topStatusCmd, "diagnostics"},
		{"versionCmd", versionCmd, "diagnostics"},
		{"scanCmd", scanCmd, "diagnostics"},
		{"mcpCmd", mcpCmd, "diagnostics"},
	}

	for _, tc := range cases {
		if got := tc.cmd.GroupID; got != tc.groupID {
			t.Errorf("%s.GroupID = %q, want %q", tc.name, got, tc.groupID)
		}
	}
}

// @s3: every assigned GroupID resolves to a real group on rootCmd (the cobra
// checkCommandGroups invariant). Asserted via ContainsGroup rather than by
// triggering cobra's panic path.
func TestNoDanglingGroupIDs(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.GroupID == "" {
			continue
		}
		if !rootCmd.ContainsGroup(c.GroupID) {
			t.Errorf("command %q has GroupID %q but rootCmd has no such group; have %v",
				c.Name(), c.GroupID, groupIDs(rootCmd.Groups()))
		}
	}
}

// @s4: deprecated commands stay ungrouped (empty GroupID). Guard so a future
// grouping change does not accidentally sweep them into a group.
func TestDeprecatedCommandsUngrouped(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"chatCmd", chatCmd},
		{"runCmd", runCmd},
		{"benchCmd", benchCmd},
		{"setupCmd", setupCmd},
	}
	for _, tc := range cases {
		if got := tc.cmd.GroupID; got != "" {
			t.Errorf("%s.GroupID = %q, want \"\" (deprecated commands stay ungrouped)", tc.name, got)
		}
	}
}

// @s5: hook-only memory plumbing subcommands are hidden from help.
func TestHookPlumbingSubcommandsHidden(t *testing.T) {
	if !memoryRelinkCmd.Hidden {
		t.Errorf("memoryRelinkCmd.Hidden = false, want true (hook-only plumbing)")
	}
	if !memoryMaterializeCmd.Hidden {
		t.Errorf("memoryMaterializeCmd.Hidden = false, want true (hook-only plumbing)")
	}
}

// @s6: user-facing memory subcommands remain visible. Guard so hiding the
// plumbing does not accidentally hide these.
func TestUserFacingMemoryCommandsVisible(t *testing.T) {
	if memoryCrystallizeCmd.Hidden {
		t.Errorf("memoryCrystallizeCmd.Hidden = true, want false (user-facing)")
	}
	if adviseCmd.Hidden {
		t.Errorf("adviseCmd.Hidden = true, want false (user-facing)")
	}
}

func groupIDs(groups []*cobra.Group) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		if g == nil {
			continue
		}
		ids = append(ids, g.ID)
	}
	return ids
}
