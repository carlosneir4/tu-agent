package tdd

import (
	"strings"
	"testing"
)

// specjudge_test.go — RED-phase tests for feature spec-judge (spec.md D5):
// a new "spec-judge" stage (role pr-reviewer) reviews spec.md + plan.md +
// the .feature files against the spec's Goal and Non-goals before the human
// gate. None of this exists yet — StageOverlay("spec-judge") returns
// ("", false) and "spec-judge" is absent from tddStages(), so every
// assertion below is honest red today. Deliberately does not reference a
// SpecJudgePrompt symbol (it does not exist yet); driven only through the
// StageOverlay/ComposeStagePrompt entry points, which compile today.
//
// Reuses readTddSkill (tddskill_review_test.go), stepThreeSection
// (planartifact_test.go), and containsFold (tddskill_review_test.go) from
// the same package rather than redefining them.

// @s1 — the composed spec-judge overlay carries the review discipline: every
// scenario must trace to the spec Goal, a Non-goals violation check, a 2-4
// line verdict, and the informs/human-decides language.
func TestStageOverlaySpecJudgeCarriesReviewDiscipline(t *testing.T) {
	overlay, _ := StageOverlay("spec-judge")

	if !containsFold(overlay, "trace to the Goal") {
		t.Error(`StageOverlay("spec-judge") must require every scenario to trace to the spec Goal (missing "trace to the Goal")`)
	}
	if !containsFold(overlay, "Non-goals") {
		t.Error(`StageOverlay("spec-judge") must require a Non-goals violation check (missing "Non-goals")`)
	}
	if !containsFold(overlay, "2-4 line") {
		t.Error(`StageOverlay("spec-judge") must ask for a 2-4 line verdict (missing "2-4 line")`)
	}
	if !containsFold(overlay, "informs") || !containsFold(overlay, "decides") {
		t.Error(`StageOverlay("spec-judge") must state the judge informs and the human decides (missing "informs"/"decides")`)
	}
}

// @s2 — spec-judge is registered as a single-source overlay: the lookup
// reports the stage is known, and (once tddStages() also carries the entry)
// ComposeStagePrompt resolves it against a repo with no materialized agents
// (embedded generic-shell fallback, mirrors TestComposeStagePromptFallbackAllStages).
func TestStageOverlaySpecJudgeRegistered(t *testing.T) {
	_, ok := StageOverlay("spec-judge")
	if !ok {
		t.Error(`StageOverlay("spec-judge") must report the stage as known (second return true)`)
	}

	root := t.TempDir()
	out, err := ComposeStagePrompt(root, "spec-judge", ".tu-agent/tdd/x")
	if err != nil {
		t.Errorf(`ComposeStagePrompt(root, "spec-judge", ...) must resolve once registered in tddStages(), got error: %v`, err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error(`ComposeStagePrompt(root, "spec-judge", ...) returned an empty prompt`)
	}
}

// @s3 — the spec-judge verdict is shown verbatim to the human, not wrapped in
// a JSON contract: the overlay must not carry the shared contractInstruction
// sentinel or ask for a fenced ```json block.
func TestStageOverlaySpecJudgeNoJSONContract(t *testing.T) {
	overlay, _ := StageOverlay("spec-judge")

	if strings.TrimSpace(overlay) == "" {
		t.Error(`StageOverlay("spec-judge") must return non-empty verdict-instruction content (the verbatim claim is vacuous over an empty overlay)`)
	}

	const jsonContractSentinel = "End your reply with a single fenced"
	if strings.Contains(overlay, jsonContractSentinel) {
		t.Errorf(`StageOverlay("spec-judge") must not contain the shared JSON-contract instruction (found %q)`, jsonContractSentinel)
	}
	if strings.Contains(overlay, "```json") {
		t.Error(`StageOverlay("spec-judge") must not ask for a fenced ` + "```json" + ` block`)
	}
}

// @s4 — SKILL.md Step 3 dispatches the spec-judge before presenting the plan
// at the human gate, shows its verdict together with plan.md, and states the
// human decides — the verdict never auto-blocks approval.
func TestTddSkillStepThreeDispatchesSpecJudge(t *testing.T) {
	s := readTddSkill(t)
	section := stepThreeSection(t, s)

	if !strings.Contains(section, "spec-judge") {
		t.Error(`tdd SKILL.md Step 3 must dispatch "spec-judge" before the human gate`)
	}
	if !strings.Contains(section, "tdd prompt spec-judge") {
		t.Error(`tdd SKILL.md Step 3 must fetch the stage prompt with "tdd prompt spec-judge"`)
	}
	if !strings.Contains(section, "plan.md") {
		t.Error(`tdd SKILL.md Step 3 must show the spec-judge verdict together with plan.md`)
	}
	if !containsFold(section, "human decides") {
		t.Error(`tdd SKILL.md Step 3 must state the human decides`)
	}
	if !containsFold(section, "never auto-block") && !containsFold(section, "never blocks") {
		t.Error(`tdd SKILL.md Step 3 must state the spec-judge verdict never auto-blocks approval`)
	}
}
