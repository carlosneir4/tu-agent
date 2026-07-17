package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRuleFile writes content to root/relPath, creating parent dirs as needed.
// Test-only helper for the F3 rules.md loader scenarios (@s1..@s6).
func writeRuleFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir parent of %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// @s1 — Absent rules files contribute nothing.
func TestLoadProjectRulesAbsent(t *testing.T) {
	root := t.TempDir()
	got := loadProjectRules(root, "developer")
	if got != "" {
		t.Fatalf("loadProjectRules on empty repo = %q, want empty string", got)
	}
}

// @s2 — Repo-wide rules.md is included under an authoritative "Project rules" header.
func TestLoadProjectRulesRepoWideHeader(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "REPO-WIDE-RULE")

	got := loadProjectRules(root, "developer")
	if !strings.Contains(got, "Project rules") {
		t.Errorf("result must contain %q, got %q", "Project rules", got)
	}
	if !strings.Contains(got, "REPO-WIDE-RULE") {
		t.Errorf("result must contain %q, got %q", "REPO-WIDE-RULE", got)
	}
	headerIdx := strings.Index(got, "Project rules")
	ruleIdx := strings.Index(got, "REPO-WIDE-RULE")
	if headerIdx == -1 || ruleIdx == -1 || headerIdx >= ruleIdx {
		t.Errorf(`"Project rules" header must appear before "REPO-WIDE-RULE", got header@%d rule@%d in %q`, headerIdx, ruleIdx, got)
	}
}

// @s3 — Per-role rules file is appended and visible to its own role, after repo-wide.
func TestLoadProjectRulesPerRoleAppended(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "REPO-WIDE-RULE")
	writeRuleFile(t, root, ".tu-agent/rules/developer.md", "DEV-ONLY-RULE")

	got := loadProjectRules(root, "developer")
	if !strings.Contains(got, "REPO-WIDE-RULE") {
		t.Errorf("result must contain %q, got %q", "REPO-WIDE-RULE", got)
	}
	if !strings.Contains(got, "DEV-ONLY-RULE") {
		t.Errorf("result must contain %q, got %q", "DEV-ONLY-RULE", got)
	}
	repoIdx := strings.Index(got, "REPO-WIDE-RULE")
	devIdx := strings.Index(got, "DEV-ONLY-RULE")
	if repoIdx == -1 || devIdx == -1 || repoIdx >= devIdx {
		t.Errorf(`"REPO-WIDE-RULE" must appear before "DEV-ONLY-RULE", got repo@%d dev@%d in %q`, repoIdx, devIdx, got)
	}
}

// @s4 — Per-role rules file is scoped out for a different role.
func TestLoadProjectRulesScopedOutForOtherRole(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/developer.md", "DEV-ONLY-RULE")

	got := loadProjectRules(root, "architect")
	if strings.Contains(got, "DEV-ONLY-RULE") {
		t.Errorf("architect result must NOT contain developer-only rule, got %q", got)
	}
}

// @s5 — Blank or whitespace-only rules files are ignored with no empty header.
func TestLoadProjectRulesBlankFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "   \n\t\n")
	writeRuleFile(t, root, ".tu-agent/rules/developer.md", "\n  \n")

	got := loadProjectRules(root, "developer")
	if got != "" {
		t.Fatalf("loadProjectRules with only whitespace files = %q, want empty string", got)
	}
	if strings.Contains(got, "Project rules") {
		t.Errorf("result must NOT contain %q when both files are blank, got %q", "Project rules", got)
	}
}

// @s6 — The loader never creates the rules files (read-only invariant).
func TestLoadProjectRulesReadOnlyInvariant(t *testing.T) {
	root := t.TempDir()

	_ = loadProjectRules(root, "developer")

	if _, err := os.Stat(filepath.Join(root, ".tu-agent", "rules", "all.md")); !os.IsNotExist(err) {
		t.Errorf(".tu-agent/rules/all.md must still not exist after loadProjectRules, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".tu-agent", "rules", "developer.md")); !os.IsNotExist(err) {
		t.Errorf(".tu-agent/rules/developer.md must still not exist after loadProjectRules, stat err = %v", err)
	}
}
