package tdd

import (
	"fmt"
	"os"
	"path/filepath"
)

// rulesReadme explains the rules subsystem so a reader learns how the
// composition works: all.md applies to every stage, and each <role>.md stacks
// on top for that role only.
const rulesReadme = `# Project rules

This directory holds the user-owned rules the tu-agent TDD dev-flow treats as
authoritative. Rules are composed per stage from two layers:

- **all.md** — the repo-wide rules file. It applies to every stage and every
  role.
- **<role>.md** — optional per-role rules that stack on top of all.md for that
  one role only. Known roles: architect, developer, qa, pr-reviewer,
  security-reviewer, analyst, scribe. For example, developer.md is added only
  when the developer stage runs.

Both files are optional and read-only to the harness — edit them freely; they
are never overwritten. This README is never read as a rules file.
`

// SeedRulesReadme seeds .tu-agent/rules/README.md explaining the rules
// subsystem. It is idempotent: if the README already exists it is left
// untouched (a user may have edited it) and (false, nil) is returned;
// otherwise it creates the rules directory, writes the README, and returns
// (true, nil).
func SeedRulesReadme(root string) (bool, error) {
	dir := filepath.Join(root, ".tu-agent", "rules")
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("tdd.SeedRulesReadme: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("tdd.SeedRulesReadme: %w", err)
	}
	if err := os.WriteFile(readme, []byte(rulesReadme), 0o644); err != nil {
		return false, fmt.Errorf("tdd.SeedRulesReadme: %w", err)
	}
	return true, nil
}
