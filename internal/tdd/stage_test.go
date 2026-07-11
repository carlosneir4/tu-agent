package tdd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDispatcher struct {
	reply string
	err   error
	gotAg string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, agent, _ string) (string, error) {
	f.gotAg = agent
	return f.reply, f.err
}

func TestStageRunnerRun(t *testing.T) {
	fd := &fakeDispatcher{reply: "done\n```json\n{\"stage\":\"architect\",\"status\":\"pass\"}\n```"}
	c, err := StageRunner{D: fd}.Run(context.Background(), "architect", "task")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fd.gotAg != "architect" {
		t.Fatalf("dispatched %q, want architect", fd.gotAg)
	}
	if c.Status != StatusPass {
		t.Fatalf("status = %q", c.Status)
	}
}

func TestStageRunnerDispatchError(t *testing.T) {
	fd := &fakeDispatcher{err: errors.New("boom")}
	if _, err := (StageRunner{D: fd}).Run(context.Background(), "judge", "t"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestToolGrants(t *testing.T) {
	if len(DefaultToolGrant) == 0 {
		t.Fatal("DefaultToolGrant must be non-empty")
	}
	hasWrite := false
	for _, n := range CraftsmanToolGrant {
		if n == "write_file" {
			hasWrite = true
		}
	}
	if !hasWrite {
		t.Fatal("CraftsmanToolGrant must include write_file")
	}
}

func TestArchitectOverlayMultiFeature(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "features") {
		t.Error("architect overlay must describe the features list")
	}
	if !strings.Contains(ArchitectPrompt, "one .feature per") && !strings.Contains(ArchitectPrompt, "one feature file per") {
		t.Error("architect overlay must tell complex to write one feature file per sub-feature")
	}
}

func TestOverlaysGraphAndPreload(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "MUST consult the graph") {
		t.Error("architect overlay must mandate graph blast-radius consultation")
	}
	if !strings.Contains(ArchitectPrompt, "WIDE blast-radius") || !strings.Contains(ArchitectPrompt, "DECOMPOSE") {
		t.Error("architect overlay must keep the complex-decompose sizing heuristic")
	}
	if !strings.Contains(AnalystPrompt, "BEFORE your first question") {
		t.Error("analyst overlay must mandate pre-loading context before interrogating")
	}
}

func TestOverlaysAreGenericContractOnly(t *testing.T) {
	overlays := map[string]string{
		"analyst": AnalystPrompt, "architect": ArchitectPrompt,
		"craftsman": CraftsmanPrompt, "judge": JudgePrompt, "scribe": ScribePrompt,
	}
	for name, o := range overlays {
		if !strings.Contains(o, "tu-agent TDD task") {
			t.Errorf("%s overlay must open by naming the tu-agent TDD task", name)
		}
		// The durable role identity now lives in the agent body, not the overlay.
		if strings.Contains(o, "senior software architect") || strings.Contains(o, "You are a senior") {
			t.Errorf("%s overlay must not carry role identity", name)
		}
	}
	if !strings.Contains(ArchitectPrompt, "@s1") {
		t.Error("architect overlay must keep the Gherkin @s contract")
	}
	if !strings.Contains(CraftsmanPrompt, "Red -> Green") {
		t.Error("craftsman overlay must keep the red-green discipline")
	}
}

func TestAnalystPromptSeedsFromDesign(t *testing.T) {
	for _, want := range []string{"design doc", "confirm by exception"} {
		if !strings.Contains(AnalystPrompt, want) {
			t.Errorf("AnalystPrompt missing %q", want)
		}
	}
}

func TestAnalystPromptDemandsPlainLanguageAndExamples(t *testing.T) {
	for _, want := range []string{"plain language", "concrete example"} {
		if !strings.Contains(AnalystPrompt, want) {
			t.Errorf("AnalystPrompt must guide developer-friendly options; missing %q", want)
		}
	}
}

func TestPromptsGlossUnfamiliarTerms(t *testing.T) {
	if !strings.Contains(AnalystPrompt, "gloss it") {
		t.Error("AnalystPrompt must require glossing unfamiliar domain terms/coined names")
	}
	if !strings.Contains(ArchitectPrompt, "gloss it") {
		t.Error("ArchitectPrompt must require glossing coined names it introduces")
	}
}

func TestCraftsmanPromptDemandsSymbolDerivedTestNames(t *testing.T) {
	for _, want := range []string{"find_symbol", "unit under test"} {
		if !strings.Contains(CraftsmanPrompt, want) {
			t.Errorf("CraftsmanPrompt must tie test names to the real symbol; missing %q", want)
		}
	}
}

func TestSandwichOverlays(t *testing.T) {
	if !strings.Contains(TestWriterPrompt, "NO production") {
		t.Error("TestWriterPrompt must forbid production code")
	}
	if !strings.Contains(ImplementerPrompt, "do NOT modify") {
		t.Error("ImplementerPrompt must forbid touching tests")
	}
	for _, p := range []string{TestWriterPrompt, ImplementerPrompt} {
		if !strings.Contains(p, "```json") {
			t.Error("overlay missing contract instruction")
		}
	}
}

func TestArchitectPromptConsultsCoverage(t *testing.T) {
	for _, want := range []string{"test gaps", "already covered"} {
		if !strings.Contains(ArchitectPrompt, want) {
			t.Errorf("ArchitectPrompt missing %q", want)
		}
	}
}

// @s1 — AnalystPrompt mandates conditional design exploration BEFORE spec.md:
// propose 2-3 graph-anchored approaches with trade-offs and a recommendation,
// human makes the final choice; trigger = the choice changes which files/packages
// are touched.
func TestAnalystPromptMandatesDesignExploration(t *testing.T) {
	// Proposes 2-3 approaches before writing spec.md, gated on multiple viable approaches.
	if !strings.Contains(AnalystPrompt, "2-3 approaches") && !strings.Contains(AnalystPrompt, "2–3 approaches") {
		t.Error("AnalystPrompt must instruct proposing 2-3 approaches when more than one viable approach exists")
	}
	if !strings.Contains(AnalystPrompt, "viable approach") {
		t.Error("AnalystPrompt must condition design exploration on more than one viable approach existing")
	}
	// The exploration happens before spec.md is written.
	if !strings.Contains(AnalystPrompt, "before writing") && !strings.Contains(AnalystPrompt, "BEFORE writing") {
		t.Error("AnalystPrompt must place design exploration BEFORE writing spec.md")
	}
	// Trigger signal phrased as the choice changing which files/packages are touched.
	if !strings.Contains(AnalystPrompt, "files") || !strings.Contains(AnalystPrompt, "packages") {
		t.Error("AnalystPrompt must state the trigger as the choice changing which files or packages are touched")
	}
	// Each approach anchored in the graph via get_context or get_impact.
	if !strings.Contains(AnalystPrompt, "get_context") || !strings.Contains(AnalystPrompt, "get_impact") {
		t.Error("AnalystPrompt must require anchoring each approach in the graph via get_context or get_impact")
	}
	// Trade-offs + a recommendation, human makes the final call.
	if !strings.Contains(AnalystPrompt, "trade-off") {
		t.Error("AnalystPrompt must require trade-offs for each approach")
	}
	if !strings.Contains(AnalystPrompt, "recommend") {
		t.Error("AnalystPrompt must require a recommendation among the approaches")
	}
	if !strings.Contains(AnalystPrompt, "human") {
		t.Error("AnalystPrompt must leave the final choice to the human")
	}
}

// @s2 — AnalystPrompt requires an always-present "## Design" section in spec.md
// recording chosen approach, why, and rejected alternatives; with only one
// reasonable approach it is a single line and no extra question is asked.
func TestAnalystPromptRequiresDesignSection(t *testing.T) {
	if !strings.Contains(AnalystPrompt, "## Design") {
		t.Error(`AnalystPrompt must require an always-present "## Design" section in spec.md`)
	}
	// Records chosen approach + why + rejected alternatives.
	if !strings.Contains(AnalystPrompt, "rejected") {
		t.Error("AnalystPrompt Design section must record the rejected alternatives")
	}
	// Single-approach path: one line, no extra question.
	if !strings.Contains(AnalystPrompt, "single line") && !strings.Contains(AnalystPrompt, "one line") {
		t.Error("AnalystPrompt must state that with one reasonable approach the Design section is a single line")
	}
	if !strings.Contains(AnalystPrompt, "no extra question") {
		t.Error("AnalystPrompt must state that with one reasonable approach no extra question is asked")
	}
}

func TestWithBaseDir(t *testing.T) {
	got := WithBaseDir("write to "+TddDirToken+"/spec.md now", ".tu-agent/tdd/ABC-1-x")
	want := "write to .tu-agent/tdd/ABC-1-x/spec.md now"
	if got != want {
		t.Fatalf("WithBaseDir = %q, want %q", got, want)
	}
}

func TestOverlayDisclaimerReplacesRoleProcess(t *testing.T) {
	overlays := map[string]string{
		"analyst": AnalystPrompt, "architect": ArchitectPrompt,
		"craftsman": CraftsmanPrompt, "judge": JudgePrompt,
		"scribe": ScribePrompt, "refactor": RefactorPrompt,
		"test-writer": TestWriterPrompt, "implementer": ImplementerPrompt,
	}
	const want = "process steps, verification commands, and definition-of-done"
	for name, o := range overlays {
		if !strings.Contains(o, want) {
			t.Errorf("%s overlay: disclaimer does not override the role's process/DoD", name)
		}
	}
	if !strings.Contains(JudgePrompt, "Do NOT re-review correctness or security") {
		t.Errorf("judge overlay lacks the explicit re-review prohibition")
	}
}

func TestPromptBatchWave2(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "features[]?") {
		t.Error("contract schema must enumerate features[]? (contractInstruction is shared by every overlay)")
	}
	if !strings.Contains(CraftsmanPrompt, "write the safety-net test by hand") {
		t.Error("CraftsmanPrompt must fall back to a hand-written safety-net test when \"tu-agent test gen\" fails")
	}
	if !strings.Contains(JudgePrompt, "score 0-10") {
		t.Error("JudgePrompt must bound score to 0-10")
	}
	if strings.Contains(ScribePrompt, "files touched") {
		t.Error("ScribePrompt must not tell the scribe to list file paths — memory relink derives links from code symbols named in prose")
	}
	if !strings.Contains(ScribePrompt, "never file paths") {
		t.Error("ScribePrompt must instruct the scribe to name code symbols in prose, never file paths, per the scribe.md convention")
	}
}

// @s1 — JudgePrompt gains a speculative-generality revise criterion:
// config/flags nobody reads, one-caller abstractions, and error handling
// for unreachable states must route to a revise verdict.
func TestJudgePromptSpeculativeGeneralityCriterion(t *testing.T) {
	if !strings.Contains(JudgePrompt, "speculative") {
		t.Error("JudgePrompt must name speculative generality as a revise criterion")
	}
	if !strings.Contains(JudgePrompt, "single caller") {
		t.Error("JudgePrompt must flag one-caller abstractions as a speculative-generality cue")
	}
	if !strings.Contains(JudgePrompt, "unread") {
		t.Error("JudgePrompt must flag config/flags nobody reads as a speculative-generality cue")
	}
	if !strings.Contains(JudgePrompt, "unreachable") {
		t.Error("JudgePrompt must flag error handling for unreachable states as a speculative-generality cue")
	}
}

// @s2 — JudgePrompt gains a surgical-discipline revise criterion: every
// changed line must trace to the task, drive-by reformat/rename/comment-churn
// is flagged as revise, and preexisting dead code is signalled, never deleted.
func TestJudgePromptSurgicalDisciplineCriterion(t *testing.T) {
	if !strings.Contains(JudgePrompt, "trace") {
		t.Error("JudgePrompt must require every changed line to trace to the task")
	}
	if !strings.Contains(JudgePrompt, "drive-by") {
		t.Error("JudgePrompt must flag drive-by reformat/rename/comment-churn as revise")
	}
	if !strings.Contains(JudgePrompt, "dead code") {
		t.Error("JudgePrompt must call out preexisting dead code")
	}
	if !strings.Contains(JudgePrompt, "signal") {
		t.Error("JudgePrompt must state preexisting dead code is signalled")
	}
	if !strings.Contains(JudgePrompt, "delet") {
		t.Error("JudgePrompt must state preexisting dead code is never deleted")
	}
}

// @s3 — the new speculative-generality/surgical-discipline criteria coexist
// with the existing no-re-review rule: design/discipline is judge territory,
// but correctness/security stays out of scope (the deterministic gate already
// proved them). Regression pin — may already pass; guards against the new
// criteria language drifting over this boundary.
func TestJudgePromptKeepsNoReReviewBoundary(t *testing.T) {
	if !strings.Contains(JudgePrompt, "Do NOT re-review correctness or security") {
		t.Error("JudgePrompt must still forbid re-reviewing correctness or security alongside the new design/discipline criteria")
	}
}

func TestOverlaysUseTokenNotLiteral(t *testing.T) {
	overlays := map[string]string{
		"analyst":   AnalystPrompt,
		"architect": ArchitectPrompt,
		"craftsman": CraftsmanPrompt,
		"judge":     JudgePrompt,
		"scribe":    ScribePrompt,
	}
	for name, ov := range overlays {
		if strings.Contains(ov, ".tu-agent/tdd/") {
			t.Errorf("%s overlay still contains hardcoded .tu-agent/tdd/ literal", name)
		}
		if !strings.Contains(ov, TddDirToken) {
			t.Errorf("%s overlay does not contain %s token", name, TddDirToken)
		}
	}
}
