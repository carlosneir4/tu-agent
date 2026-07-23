package tdd

import (
	"strings"
	"testing"
)

// RED-phase tests for the SessionStart rules hook feature (@s1-@s4): the
// exported reader tdd.SessionRules does not exist yet, so this file fails to
// compile against the pre-change tree — an acceptable RED for a brand-new
// exported symbol. Mirrors the tempdir+writeRuleFile pattern from
// rules_test.go (loadProjectRules coverage), reusing its writeRuleFile helper
// since these live in the same package.

// @s1 — SessionRules returns empty when all.md is absent.
func TestSessionRulesAbsent(t *testing.T) {
	root := t.TempDir()

	got := SessionRules(root)
	if got != "" {
		t.Fatalf("SessionRules on repo with no all.md = %q, want empty string", got)
	}
}

// @s2 — SessionRules returns empty when all.md is whitespace-only.
func TestSessionRulesWhitespaceOnly(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "   \n\t\n")

	got := SessionRules(root)
	if got != "" {
		t.Fatalf("SessionRules with whitespace-only all.md = %q, want empty string", got)
	}
}

// @s3 — SessionRules returns the rules under the authoritative header.
func TestSessionRulesReturnsHeaderAndBody(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "REPO-WIDE-RULE")

	got := SessionRules(root)
	if !strings.Contains(got, "Project rules") {
		t.Errorf("result must contain header %q, got %q", "Project rules", got)
	}
	if !strings.Contains(got, "REPO-WIDE-RULE") {
		t.Errorf("result must contain %q, got %q", "REPO-WIDE-RULE", got)
	}
}

// @s4 — SessionRules ignores per-role files: only developer.md present (no
// all.md) still yields "".
func TestSessionRulesIgnoresPerRoleFiles(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/developer.md", "DEV-ONLY-RULE")

	got := SessionRules(root)
	if got != "" {
		t.Fatalf("SessionRules with only developer.md present = %q, want empty string (role \"\" reads all.md only)", got)
	}
}
