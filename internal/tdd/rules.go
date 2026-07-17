package tdd

import (
	"log/slog"
	"os"
	"strings"
)

// rulesHeader marks the user-owned project rules block as authoritative: the
// judge and stage agents must treat a violation as a defect, not a suggestion.
const rulesHeader = "## Project rules (.tu-agent/rules/all.md) — user-owned, authoritative\n" +
	"These rules are binding; a violation is grounds to revise.\n\n"

// readRulesFile reads path and returns its trimmed content. An absent file is
// not an error — rules are optional, so it yields "". Any OTHER read error
// (e.g. a rules file present but unreadable) also yields "" so the flow never
// breaks, but is logged: silently dropping the user's rules would disable judge
// enforcement without a signal.
func readRulesFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("skipping unreadable project rules file", "path", path, "err", err)
		}
		return ""
	}
	return strings.TrimSpace(string(data))
}

// loadProjectRules returns the user-owned project rules for a role, ready to
// splice between an agent body and its stage overlay in composeStagePrompt.
//
// It reads the repo-wide .tu-agent/rules/all.md, then (if role != "") the optional
// per-role .tu-agent/rules/<role>.md, and joins the non-empty parts under an
// authoritative header. It never creates files or directories — read-only.
func loadProjectRules(root, role string) string {
	parts := make([]string, 0, 2)

	if repoWide := readRulesFile(rulesPath(root)); repoWide != "" {
		parts = append(parts, repoWide)
	}
	if role != "" {
		if perRole := readRulesFile(roleRulesPath(root, role)); perRole != "" {
			parts = append(parts, perRole)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return rulesHeader + strings.Join(parts, "\n\n")
}
