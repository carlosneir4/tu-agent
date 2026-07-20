package codegen

// Tests for git-merge-rebase-ask-tier (@s1, @s2): hardenAsk
// (internal/codegen/settings_gen.go) must gate local git merge and git rebase
// behind human confirmation, same as the existing write-side git patterns
// (add/commit/branch/checkout/push). See
// .tu-agent/tdd/untrusted-repo-local-test-command-guard/features/git-merge-rebase-ask-tier.feature
//
// Reuses the permList/contains helpers already defined in harden_test.go
// (same package) rather than re-deriving how to extract permissions lists
// from the map-shaped HardenedSettings output.

import "testing"

// TestHardenGitMergeRebase_AskContainsMerge is scenario @s1: generated
// settings must gate git merge behind the ask tier.
func TestHardenGitMergeRebase_AskContainsMerge(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	ask := permList(t, s, "ask")
	want := "Bash(git merge *)"
	if !contains(ask, want) {
		t.Errorf("permissions.ask must contain %q (git merge requires human confirmation)", want)
	}
}

// TestHardenGitMergeRebase_AskContainsRebase is scenario @s2: generated
// settings must gate git rebase behind the ask tier.
func TestHardenGitMergeRebase_AskContainsRebase(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	ask := permList(t, s, "ask")
	want := "Bash(git rebase *)"
	if !contains(ask, want) {
		t.Errorf("permissions.ask must contain %q (git rebase requires human confirmation)", want)
	}
}
