package tdd

import (
	"strings"
	"testing"
)

// planartifact_test.go — RED-phase tests for feature
// plan-artifact-and-gherkin-prompts (spec.md D1 + D2): ArchitectPrompt
// (internal/tdd/stage.go) gains the plan.md authoring instructions and the
// Gherkin readability rules, and the plugin conductor's SKILL.md presents
// plan.md at the human gate (Step 3), gates approval on approve-design,
// presents only the delta on rejection rounds, passes --complexity at begin
// (Step 5), and re-validates the remaining plan on resume (Step 0). None of
// this language exists yet in either surface, so every assertion below is
// honest red today.
//
// Reuses readTddSkill (tddskill_review_test.go) and stepZeroSection
// (tddskill_autofix_test.go) from the same package rather than redefining
// them.

// stepThreeSection slices the Step 3 (human gate) content out of the full
// SKILL.md body, anchored on the "## Step 3" heading through the next "## "
// heading — mirrors stepZeroSection/reviewSection's slicing convention. The
// anchor matches "## Step 3: Human gate ..." and stops at "## Step 3.5: ...",
// which also satisfies "\n## " and correctly bounds the section.
func stepThreeSection(t *testing.T, s string) string {
	t.Helper()
	const anchor = "\n## Step 3"
	i := strings.Index(s, anchor)
	if i < 0 {
		t.Fatalf(`tdd SKILL.md must have an h2 "## Step 3" heading for the human gate (found no "\n## Step 3")`)
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

// stepFiveSection slices the Step 5 (standard loop) content out of the full
// SKILL.md body, anchored on the "## Step 5" heading through the next "## "
// heading — mirrors stepZeroSection/reviewSection's slicing convention.
func stepFiveSection(t *testing.T, s string) string {
	t.Helper()
	const anchor = "\n## Step 5"
	i := strings.Index(s, anchor)
	if i < 0 {
		t.Fatalf(`tdd SKILL.md must have an h2 "## Step 5" heading for the standard loop (found no "\n## Step 5")`)
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

// @s1 — ArchitectPrompt instructs writing plan.md for standard and complex
// only (never trivial), in ENGLISH, with an "ONE LINE per scenario" rule and
// an empty "Sign-off" section.
func TestArchitectPromptInstructsPlanMdForStandardAndComplexOnly(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "plan.md") {
		t.Error(`ArchitectPrompt must instruct writing "plan.md"`)
	}
	if !strings.Contains(ArchitectPrompt, "ENGLISH") {
		t.Error(`ArchitectPrompt must require the plan.md to be written in "ENGLISH"`)
	}
	if !strings.Contains(ArchitectPrompt, "never trivial") {
		t.Error(`ArchitectPrompt must state the plan.md instructions apply "never trivial"`)
	}
	if !strings.Contains(ArchitectPrompt, "ONE LINE per scenario") {
		t.Error(`ArchitectPrompt must require "ONE LINE per scenario" in the plan.md's Features & scenarios section`)
	}
	if !strings.Contains(ArchitectPrompt, "Sign-off") {
		t.Error(`ArchitectPrompt must mention a "Sign-off" section in the plan.md`)
	}
}

// @s2 — ArchitectPrompt carries the Gherkin readability rules: one behavior
// per scenario, at most five steps, plain-language titles, and that scenario
// prose may follow the session language.
func TestArchitectPromptCarriesGherkinReadabilityRules(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "one behavior per scenario") {
		t.Error(`ArchitectPrompt must state "one behavior per scenario"`)
	}
	if !strings.Contains(ArchitectPrompt, "five steps") {
		t.Error(`ArchitectPrompt must state a "five steps" limit`)
	}
	if !strings.Contains(ArchitectPrompt, "plain-language") {
		t.Error(`ArchitectPrompt must require "plain-language" scenario titles`)
	}
	if !strings.Contains(ArchitectPrompt, "session language") {
		t.Error(`ArchitectPrompt must permit scenario prose to follow the "session language"`)
	}
}

// @s3 — SKILL.md Step 3 presents plan.md instead of raw Gherkin, and on
// approval runs approve-design with --rounds and saves a decision/ memory
// note at plan time.
func TestTddSkillStepThreePresentsPlanMdAndGatesApprovalOnApproveDesign(t *testing.T) {
	s := readTddSkill(t)
	section := stepThreeSection(t, s)

	if !strings.Contains(section, "plan.md") {
		t.Error("tdd SKILL.md Step 3 must present plan.md instead of raw Gherkin")
	}
	if !containsFold(section, "raw gherkin") {
		t.Error(`tdd SKILL.md Step 3 must contrast presenting plan.md against "raw Gherkin"`)
	}
	if !strings.Contains(section, "approve-design") {
		t.Error("tdd SKILL.md Step 3 must run approve-design on approval")
	}
	if !strings.Contains(section, "--rounds") {
		t.Error("tdd SKILL.md Step 3 must pass --rounds to approve-design")
	}
	if !strings.Contains(section, "decision/") {
		t.Error("tdd SKILL.md Step 3 must save a decision/ memory note at plan time")
	}
}

// @s4 — SKILL.md Step 3 presents only the delta of a revised design on
// rejection rounds, not a reprint of everything.
func TestTddSkillStepThreePresentsDeltaOnRejectionRounds(t *testing.T) {
	s := readTddSkill(t)
	section := stepThreeSection(t, s)

	if !containsFold(section, "delta") {
		t.Error("tdd SKILL.md Step 3 must tell the conductor to present the delta of a revised design on rejection rounds")
	}
	if !containsFold(section, "not") || !containsFold(section, "reprint") {
		t.Error(`tdd SKILL.md Step 3 must state not to reprint everything on a rejection round (missing "reprint")`)
	}
}

// @s5 — SKILL.md Step 5 passes the architect's complexity to the fresh-run
// tdd state begin call.
func TestTddSkillStepFivePassesComplexityToBegin(t *testing.T) {
	s := readTddSkill(t)
	section := stepFiveSection(t, s)

	if !strings.Contains(section, "tdd state begin") {
		t.Error("tdd SKILL.md Step 5 must contain the tdd state begin instruction")
	}
	if !strings.Contains(section, "--complexity") {
		t.Error("tdd SKILL.md Step 5's tdd state begin instruction must include --complexity")
	}
}

// @s6 — SKILL.md Step 0's resume path re-presents the pending features from
// plan.md and asks the user "still valid?" before jumping into the feature
// loop.
func TestTddSkillStepZeroResumeRevalidatesRemainingPlan(t *testing.T) {
	s := readTddSkill(t)
	section := stepZeroSection(t, s)

	if !strings.Contains(section, "plan.md") {
		t.Error("tdd SKILL.md Step 0 resume path must re-present the pending features from plan.md")
	}
	if !containsFold(section, "still valid?") {
		t.Error(`tdd SKILL.md Step 0 resume path must ask the user "still valid?"`)
	}
}
