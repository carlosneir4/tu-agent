package codegen

// Tests for harden-git-policy (@f0-5): the harness never commits on its own.
// hardenAllow/hardenAsk must stop pre-approving git writes, and hardenDeny
// must hard-block irreversible git operations. See
// .tu-agent/tdd/f0-security-hardening-fixes-plan-version-es/features/harden-git-policy.feature
//
// Reuses the permList/contains helpers already defined in harden_test.go
// (same package) rather than re-deriving how to extract permissions lists
// from the map-shaped HardenedSettings output.

import "testing"

// gitWritePatterns are the four git operations that must move out of allow
// and into ask per the product rule "the harness never commits".
var gitWritePatterns = []string{
	"Bash(git add *)", "Bash(git commit *)", "Bash(git branch *)", "Bash(git checkout *)",
}

// TestHardenGitPolicy_AllowNoLongerHasWriteGit is scenario @s1: git
// add/commit/branch/checkout must no longer be pre-approved in allow.
func TestHardenGitPolicy_AllowNoLongerHasWriteGit(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	allow := permList(t, s, "allow")
	for _, pattern := range gitWritePatterns {
		if contains(allow, pattern) {
			t.Errorf("permissions.allow must NOT contain %q (git writes require confirmation, not pre-approval)", pattern)
		}
	}
}

// TestHardenGitPolicy_AskRequiresConfirmationForWriteGit is scenario @s2: git
// add/commit/branch/checkout must require human confirmation via ask.
func TestHardenGitPolicy_AskRequiresConfirmationForWriteGit(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	ask := permList(t, s, "ask")
	for _, pattern := range gitWritePatterns {
		if !contains(ask, pattern) {
			t.Errorf("permissions.ask must contain %q (git writes require human confirmation)", pattern)
		}
	}
}

// TestHardenGitPolicy_DenyHardBlocksDestructiveGit is scenario @s3:
// irreversible git reset/clean must be hard-denied, not merely ask-gated.
func TestHardenGitPolicy_DenyHardBlocksDestructiveGit(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	deny := permList(t, s, "deny")
	for _, pattern := range []string{"Bash(git reset --hard*)", "Bash(git clean -fd*)"} {
		if !contains(deny, pattern) {
			t.Errorf("permissions.deny must contain %q (irreversible git op)", pattern)
		}
	}
}

// TestHardenGitPolicy_ReadOnlyGitStillAllowed is scenario @s4: read-only git
// (status/diff/log) must remain pre-approved in allow, unaffected by the
// write-git policy change.
func TestHardenGitPolicy_ReadOnlyGitStillAllowed(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	allow := permList(t, s, "allow")
	for _, pattern := range []string{"Bash(git status*)", "Bash(git diff*)", "Bash(git log*)"} {
		if !contains(allow, pattern) {
			t.Errorf("permissions.allow must still contain %q (read-only git stays pre-approved)", pattern)
		}
	}
}
