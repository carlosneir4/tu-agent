package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTddSkill loads the plugin conductor's SKILL.md, shared by @s1-@s3.
func readTddSkill(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "tdd", "SKILL.md"))
	if err != nil {
		t.Fatalf("read tdd SKILL.md: %v", err)
	}
	return string(raw)
}

// reviewSection slices the review-step content out of the full SKILL.md body,
// anchored on the "## Step 6" heading itself (through the next "## " heading,
// or end of file). Every other step in the doc is an h2 ("## Step 5", "## Step
// 4", ...) — Step 6 must match that convention, not read as a nested
// subsection of Step 5's inner loop. The anchor requires a leading newline
// immediately followed by exactly "## " (two hashes) so an "### Step 6" (h3)
// heading does NOT satisfy it — promoting the heading level is part of the
// fix under test, so a still-h3 heading must fail loudly here rather than
// silently falling back to the whole document.
func reviewSection(t *testing.T, s string) string {
	t.Helper()
	const anchor = "\n## Step 6"
	i := strings.Index(s, anchor)
	if i < 0 {
		t.Fatalf(`tdd SKILL.md must have an h2 "## Step 6" heading for the post-loop review step (found no "\n## Step 6"); a "### Step 6" (h3) subsection heading does not satisfy the doc's step convention`)
	}
	rest := s[i+1:] // drop the leading newline, keep from "## Step 6" onward
	// Skip past the Step 6 heading line itself so we don't just re-match it.
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// containsFold is a case-insensitive strings.Contains, used where the exact
// casing of prose (e.g. mirroring review.go's "SUITE was left RED" or a
// lowercase paraphrase) is not load-bearing but the substance is.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// @s1 — SKILL.md documents when the post-loop review runs: once after all
// features pass, skipped on trivial/blocked, fetched via `tdd prompt review`.
func TestTddSkillReviewStepRunsOncePostLoop(t *testing.T) {
	s := readTddSkill(t)
	section := reviewSection(t, s)

	for _, want := range []string{
		"review",
		"once",
		"after all features",
		"pass",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("tdd SKILL.md review step must mention %q (once-after-all-features-pass); section:\n%s", want, section)
		}
	}

	if !strings.Contains(section, "trivial") {
		t.Error("tdd SKILL.md review step must state it is skipped on the trivial path")
	}
	if !strings.Contains(section, "blocked") {
		t.Error("tdd SKILL.md review step must state it is skipped when any feature is blocked")
	}
	if !strings.Contains(section, `tdd prompt review --base "$BASE"`) {
		t.Error(`tdd SKILL.md review step must fetch the stage prompt with tdd prompt review --base "$BASE"`)
	}
}

// @s2 — SKILL.md documents finding routing: critical/important -> fixer with
// a budget of 2 rounds, `tdd verify` ok true gate, minor is report-only,
// persistent criticals end the run pass-with-pending-findings, the report
// path is $BASE/progress/review.md (written by the dispatched review stage,
// not the conductor), and the inner red-suite re-feed loop is itself bounded
// (2 re-feeds) with a give-up path that does NOT re-review, surfaces a
// suite-left-red warning, and still records the review outcome — matching
// internal/tdd/review.go's reviewRefeedBudget and runFixerRound/
// runReviewRounds give-up branch (§10 identical-behavior).
func TestTddSkillReviewStepDocumentsRoutingAndBudget(t *testing.T) {
	s := readTddSkill(t)
	section := reviewSection(t, s)

	for _, want := range []string{
		"critical",
		"important",
		"review-fixer",
		"budget",
		"2",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("tdd SKILL.md review step must document fixer routing/budget; missing %q", want)
		}
	}
	if !strings.Contains(section, "tdd verify") {
		t.Error("tdd SKILL.md review step must require running `tdd verify` after each fixer round")
	}
	if !strings.Contains(section, "ok") || !strings.Contains(section, "true") {
		t.Error(`tdd SKILL.md review step must require "ok": true after each fixer round`)
	}
	if !strings.Contains(section, "minor") {
		t.Error("tdd SKILL.md review step must state minor findings are report-only")
	}
	if !strings.Contains(section, "report-only") {
		t.Error("tdd SKILL.md review step must state minor findings are report-only (literal phrase)")
	}
	if !strings.Contains(section, "pending") {
		t.Error("tdd SKILL.md review step must state persistent criticals end the run pass-with-pending-findings")
	}
	if !strings.Contains(section, "$BASE/progress/review.md") {
		t.Error("tdd SKILL.md review step must name $BASE/progress/review.md as the report path")
	}
	if !containsFold(section, "stage writes") {
		t.Error("tdd SKILL.md review step must attribute writing $BASE/progress/review.md to the dispatched review stage, not the conductor (e.g. \"the dispatched review stage writes ...\")")
	}

	// The inner red-suite re-feed loop must itself be bounded (mirrors
	// reviewRefeedBudget = 2 in internal/tdd/review.go), and exhausting it
	// must NOT re-dispatch the review — it must give up with a visible
	// suite-left-red warning and still record the review outcome.
	if !containsFold(section, "gives up") {
		t.Error(`tdd SKILL.md review step must state the fixer re-feed loop gives up when exhausted (bounded re-feed, mirrors reviewRefeedBudget=2 in internal/tdd/review.go)`)
	}
	if !containsFold(section, "without re-review") {
		t.Error(`tdd SKILL.md review step must state that on re-feed exhaustion the conductor does NOT re-dispatch the review (no re-review while the suite is still red)`)
	}
	if !containsFold(section, "left red") {
		t.Error(`tdd SKILL.md review step must surface a suite-left-red warning when the re-feed budget is exhausted (mirrors review.go's "the test SUITE was left RED by the review-fixer")`)
	}
	if !containsFold(section, "still record") {
		t.Error(`tdd SKILL.md review step must state the review outcome is still recorded (e.g. "tdd state review pass") even when a round gave up with the suite left red`)
	}
}

// @s3 — SKILL.md documents skip conditions (merge-base fails / empty diff),
// the correct branch-scope recipe (committed diff since merge-base — NOT the
// untracked-aware working-tree set used by the RED/GREEN phases), state
// recording via `tdd state review`, and resume landing on the review.
func TestTddSkillReviewStepDocumentsSkipStateAndResume(t *testing.T) {
	s := readTddSkill(t)
	section := reviewSection(t, s)

	for _, want := range []string{
		"merge-base",
		"empty",
		"warning",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("tdd SKILL.md review step must document the skip condition; missing %q", want)
		}
	}
	if !strings.Contains(section, `tdd state review`) {
		t.Error("tdd SKILL.md review step must record the outcome with `tdd state review`")
	}
	if !strings.Contains(section, "skipped") {
		t.Error("tdd SKILL.md review step must state the recorded outcome can be skipped")
	}

	// Correct scope recipe: the committed branch diff since merge-base against
	// the default branch (internal/tdd/reviewscope.go: defaultBranch prefers
	// origin/HEAD, then main, then master; DiffFiles runs
	// `git diff --name-only <merge-base> HEAD`). This is NOT the RED/GREEN
	// phases' untracked-aware working-tree set
	// (`git diff --name-only; git ls-files --others --exclude-standard`).
	for _, want := range []string{
		"git merge-base",
		"git diff --name-only",
		"HEAD",
		"origin/HEAD",
		"master",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("tdd SKILL.md review step must document the committed-diff scope recipe; missing %q (mirrors internal/tdd/reviewscope.go)", want)
		}
	}
	// The old wrong parenthetical pointed at the RED/GREEN phases' recipe
	// (untracked-aware working-tree set); it must not survive within the
	// Step 6 section once the correct recipe replaces it.
	for _, unwanted := range []string{
		"untracked-aware",
		"RED/GREEN phases",
		"git ls-files --others",
	} {
		if strings.Contains(section, unwanted) {
			t.Errorf("tdd SKILL.md review step must not point at the RED/GREEN untracked-aware working-tree recipe; found unwanted %q in section:\n%s", unwanted, section)
		}
	}

	// Resume instructions: `tdd status` showing all features pass and review
	// pending must land on the review. This guidance may live in the review
	// step itself or in the surrounding resume/status guidance, so search the
	// whole document rather than the sliced section.
	for _, want := range []string{
		"tdd status",
		"review",
		"pending",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("tdd SKILL.md must document resume landing on the review via `tdd status`; missing %q", want)
		}
	}
}
