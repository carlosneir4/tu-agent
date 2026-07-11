package tdd

import (
	"strings"
	"testing"
)

// @s1 — ReviewPrompt carries the whole-branch review contract.
func TestReviewPromptWholeBranchContract(t *testing.T) {
	p := ReviewPrompt

	// Scopes the review to the whole-branch diff from the git merge-base with
	// the default branch.
	if !strings.Contains(p, "merge-base") {
		t.Error("ReviewPrompt must scope the review to the git merge-base")
	}
	if !strings.Contains(p, "default branch") {
		t.Error("ReviewPrompt must reference the default branch for the merge-base")
	}
	if !strings.Contains(p, "whole branch") && !strings.Contains(p, "whole-branch") {
		t.Error("ReviewPrompt must scope the review to the whole branch diff")
	}

	// Directs the review at what the per-feature judge cannot see.
	for _, want := range []string{"correctness", "security", "cross-feature"} {
		if !strings.Contains(p, want) {
			t.Errorf("ReviewPrompt must direct the review at %q", want)
		}
	}
	if !strings.Contains(p, "judge") {
		t.Error("ReviewPrompt must contrast itself with the per-feature judge it complements")
	}

	// Requires findings with severity critical|important|minor, each with
	// file:line and a summary.
	if !strings.Contains(p, "findings") {
		t.Error("ReviewPrompt must require a findings list")
	}
	for _, sev := range []string{"critical", "important", "minor"} {
		if !strings.Contains(p, sev) {
			t.Errorf("ReviewPrompt must enumerate severity %q", sev)
		}
	}
	if !strings.Contains(p, "file:line") {
		t.Error("ReviewPrompt must require each finding to carry a file:line location")
	}
	if !strings.Contains(p, "summary") {
		t.Error("ReviewPrompt must require each finding to carry a summary")
	}

	// Report written to the __TDDDIR__ token path under progress/review.md.
	if !strings.Contains(p, TddDirToken) {
		t.Errorf("ReviewPrompt must use the %s token for the report path", TddDirToken)
	}
	if !strings.Contains(p, "progress/review.md") {
		t.Error("ReviewPrompt must write its report to progress/review.md under the base dir")
	}

	// Verdict restricted to pass or revise.
	if !strings.Contains(p, "pass") || !strings.Contains(p, "revise") {
		t.Error("ReviewPrompt must restrict the verdict to pass or revise")
	}

	// Shared stage disclaimer + fenced json contract instruction.
	if !strings.Contains(p, "process steps, verification commands, and definition-of-done") {
		t.Error("ReviewPrompt must carry the shared stage disclaimer")
	}
	if !strings.Contains(p, "```json") {
		t.Error("ReviewPrompt must embed the fenced json contract instruction")
	}
}

// @s2 — ReviewFixerPrompt forbids touching tests and demands a green suite.
func TestReviewFixerPromptForbidsTestsRequiresGreen(t *testing.T) {
	p := ReviewFixerPrompt

	// Forbids modifying, adding, or deleting any test file.
	if !strings.Contains(p, "test file") {
		t.Error("ReviewFixerPrompt must reference test files it may not touch")
	}
	for _, verb := range []string{"modify", "add", "delete"} {
		if !strings.Contains(p, verb) {
			t.Errorf("ReviewFixerPrompt must forbid %sing test files", verb)
		}
	}

	// The entire suite must remain green after the fix.
	if !strings.Contains(p, "green") {
		t.Error("ReviewFixerPrompt must require the suite to remain green after the fix")
	}

	// Reports the primary source file as a {"kind":"source"} artifact.
	if !strings.Contains(p, `"kind":"source"`) {
		t.Error(`ReviewFixerPrompt must require reporting the primary source file as a {"kind":"source"} artifact`)
	}

	// Shared disclaimer + contract instruction + __TDDDIR__ token.
	if !strings.Contains(p, "process steps, verification commands, and definition-of-done") {
		t.Error("ReviewFixerPrompt must carry the shared stage disclaimer")
	}
	if !strings.Contains(p, "```json") {
		t.Error("ReviewFixerPrompt must embed the fenced json contract instruction")
	}
	if !strings.Contains(p, TddDirToken) {
		t.Errorf("ReviewFixerPrompt must use the %s token", TddDirToken)
	}
}

// @s3 — review findings parse through the existing contract envelope: a
// verdict result "revise" with findings [{severity, location, summary}] yields a
// Verdict carrying the findings with severities intact; a contract without
// findings still parses exactly as before.
func TestParseContractCarriesReviewFindings(t *testing.T) {
	reply := "reviewed the branch\n\n```json\n" +
		`{"stage":"review","status":"revise","verdict":{"result":"revise","findings":[` +
		`{"severity":"critical","location":"internal/tdd/flow.go:42","summary":"nil deref on empty diff"},` +
		`{"severity":"minor","location":"cmd/tu-agent/tdd.go:58","summary":"stale comment"}]}}` +
		"\n```\n"

	c, err := ParseContract(reply)
	if err != nil {
		t.Fatalf("ParseContract: %v", err)
	}
	if c.Verdict == nil {
		t.Fatal("parsed contract must carry a verdict")
	}
	if c.Verdict.Result != "revise" {
		t.Fatalf("verdict result = %q, want revise", c.Verdict.Result)
	}
	if len(c.Verdict.Findings) != 2 {
		t.Fatalf("verdict carries %d findings, want 2", len(c.Verdict.Findings))
	}
	f := c.Verdict.Findings[0]
	if f.Severity != "critical" {
		t.Errorf("finding[0].Severity = %q, want critical", f.Severity)
	}
	if f.Location != "internal/tdd/flow.go:42" {
		t.Errorf("finding[0].Location = %q, want internal/tdd/flow.go:42", f.Location)
	}
	if f.Summary != "nil deref on empty diff" {
		t.Errorf("finding[0].Summary = %q, want the summary text", f.Summary)
	}
	if c.Verdict.Findings[1].Severity != "minor" {
		t.Errorf("finding[1].Severity = %q, want minor", c.Verdict.Findings[1].Severity)
	}
}

func TestParseContractWithoutFindingsBackwardCompat(t *testing.T) {
	// A judge-shaped contract with a verdict but no findings must parse exactly
	// as before: no error, empty findings.
	reply := "```json\n" +
		`{"stage":"judge","status":"pass","verdict":{"result":"pass","feedback":"clean","score":9}}` +
		"\n```"
	c, err := ParseContract(reply)
	if err != nil {
		t.Fatalf("ParseContract (no findings): %v", err)
	}
	if c.Verdict == nil {
		t.Fatal("verdict must still parse")
	}
	if len(c.Verdict.Findings) != 0 {
		t.Fatalf("verdict without findings must yield 0 findings, got %d", len(c.Verdict.Findings))
	}
	if c.Verdict.Score != 9 || c.Verdict.Feedback != "clean" {
		t.Errorf("legacy verdict fields must survive: %+v", c.Verdict)
	}
}
