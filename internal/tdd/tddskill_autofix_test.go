package tdd

import (
	"strings"
	"testing"
)

// tddskill_autofix_test.go — RED-phase test for feature auto-fix-review-flag,
// scenario @s5 (spec.md section B, design.md D3): the plugin conductor's
// SKILL.md must document the tdd.auto_fix_review flag in Step 0's config
// notes, and Step 6 must gate review-fixer dispatch on a human approval step
// — approve dispatches the fixer for the approved subset of findings only,
// reject/defer leaves findings recorded in progress/review.md with a
// pass-with-pending outcome. Neither exists in SKILL.md yet, so this is a
// pure grep test — compile-safe by construction, red today because the
// language is absent.
//
// Reuses readTddSkill and reviewSection from tddskill_review_test.go (same
// package) rather than redefining them.

// stepZeroSection slices the Step 0 (Preflight) content out of the full
// SKILL.md body, anchored on the "## Step 0" heading through the next "## "
// heading (mirrors reviewSection's slicing convention for Step 6).
func stepZeroSection(t *testing.T, s string) string {
	t.Helper()
	const anchor = "\n## Step 0"
	i := strings.Index(s, anchor)
	if i < 0 {
		t.Fatalf(`tdd SKILL.md must have an h2 "## Step 0" heading for preflight (found no "\n## Step 0")`)
	}
	rest := s[i+1:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// @s5 — the plugin skill documents the human gate for tdd.auto_fix_review:
// Step 0 notes the flag next to the other config notes, and Step 6 gates
// fixer dispatch on human approval of the approved subset, leaving rejected
// findings recorded with a pass-with-pending outcome.
func TestTddSkillDocumentsAutoFixReviewHumanGate(t *testing.T) {
	s := readTddSkill(t)

	step0 := stepZeroSection(t, s)
	for _, want := range []string{"tdd.auto_fix_review", "tdd.mutation", "tdd.archive"} {
		if !strings.Contains(step0, want) {
			t.Errorf("tdd SKILL.md Step 0 must mention %q next to the other config notes; section:\n%s", want, step0)
		}
	}

	section := reviewSection(t, s)
	if !containsFold(section, "human") {
		t.Error("tdd SKILL.md Step 6 must gate fixer dispatch on a human approval step (missing \"human\")")
	}
	if !containsFold(section, "approv") {
		t.Error("tdd SKILL.md Step 6 must reference approval of findings before dispatching the fixer (missing \"approv\")")
	}
	if !strings.Contains(section, "approved subset") {
		t.Error(`tdd SKILL.md Step 6 must dispatch the review-fixer for the "approved subset" of findings only`)
	}
	if !containsFold(section, "reject") {
		t.Error("tdd SKILL.md Step 6 must document that rejected/deferred findings stay recorded, not auto-fixed (missing \"reject\")")
	}
	if !strings.Contains(section, "progress/review.md") {
		t.Error("tdd SKILL.md Step 6 must state rejected findings stay recorded in progress/review.md")
	}
	if !containsFold(section, "pass-with-pending") {
		t.Error(`tdd SKILL.md Step 6 must record a pass-with-pending outcome when findings are rejected/deferred`)
	}
}
