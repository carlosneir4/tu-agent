package tdd

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// review_autofix_test.go — RED-phase tests for feature auto-fix-review-flag,
// scenarios @s3 and @s4 (spec.md section B, design.md D2). tdd.Options does
// not carry an AutoFixReview field yet, so @s4 (which needs it set true)
// reaches it through reflection rather than naming it directly — a direct
// field reference would fail to COMPILE today, but the RED gate requires
// today's failure to be a runtime/assertion failure. @s3 needs no reflection:
// its default (flag "off") is the bool zero value, so it drives the existing
// scripted reviewFlowDispatcher exactly as review_flow_test.go's other
// default-options scenarios do, and fails today because runReviewRounds
// dispatches the review-fixer unconditionally (the gating does not exist
// yet).

// reviewReviseCriticalAndMinor scripts a review verdict carrying one critical
// and one minor finding — @s3/@s4's "1 critical and 1 minor finding".
func reviewReviseCriticalAndMinor(criticalSummary, minorSummary string) string {
	return jsonBlock(`{"stage":"review","status":"revise","verdict":{"result":"revise","findings":[` +
		`{"severity":"critical","location":"internal/tdd/flow.go:42","summary":"` + criticalSummary + `"},` +
		`{"severity":"minor","location":"internal/tdd/flow.go:10","summary":"` + minorSummary + `"}]}}`)
}

// setAutoFixReviewOption sets Options.AutoFixReview to true via reflection,
// failing the test with a clear message when the field is not yet present —
// today's honest RED for @s4. Once tdd.Options grows the field (design.md
// D2), this resolves and flips the flag exactly as a direct field set would.
func setAutoFixReviewOption(t *testing.T, o *Options) {
	t.Helper()
	f := reflect.ValueOf(o).Elem().FieldByName("AutoFixReview")
	if !f.IsValid() {
		t.Fatalf("tdd.Options has no AutoFixReview field yet (spec.md section B, design.md D2) — RED until the GREEN change adds `AutoFixReview bool` wired from cfg.Tdd.AutoFixReview")
	}
	f.SetBool(true)
}

// @s3 — flag off (the default, zero value) leaves a critical finding unfixed:
// the review-fixer is never dispatched, the output presents the per-severity
// counts and the progress/review.md pointer, and the run still ends pass with
// state review "pass".
func TestAutoFixReviewOff_LeavesCriticalUnfixedAndPresentsFindings(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		// A second reply is harmless slack: today's unfixed code still
		// re-reviews after dispatching the fixer, so it needs one. Once the
		// GREEN change gates the fixer off, only the first reply is consumed.
		"review":       {reviewReviseCriticalAndMinor("nil deref on empty diff", "stale comment"), reviewPass()},
		"review-fixer": {contractWithSource("internal/tdd/flow.go", "@s1")},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	// AutoFixReview intentionally left at its zero value (false) — the default.

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review-fixer"] != 0 {
		t.Fatalf("review-fixer dispatched %d times, want 0 when AutoFixReview is off (default)", d.calls["review-fixer"])
	}
	msg := strings.ToLower(out.String())
	if !strings.Contains(msg, "critical") {
		t.Errorf("output = %q, want it to present the critical-finding count", out.String())
	}
	if !strings.Contains(msg, "minor") {
		t.Errorf("output = %q, want it to present the minor-finding count", out.String())
	}
	if !strings.Contains(out.String(), "progress/review.md") {
		t.Errorf("output = %q, want it to point at the persisted progress/review.md findings file", out.String())
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (unfixed findings never block a run whose features already passed their gates)", res.Status)
	}
	if got := loadReview(t, opts.WorkDir); got != "pass" {
		t.Fatalf("persisted state.Review = %q, want pass", got)
	}
}

// @s4 — flag on restores the auto-fix loop unchanged: the review-fixer is
// dispatched exactly once, a clean re-review resolves it, and the run passes
// with the existing "review complete: no blocking findings." message.
func TestAutoFixReviewOn_RestoresFixerLoopUnchanged(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect":    {rvArch},
		"craftsman":    {rvCraft},
		"judge":        {rvJudgePass},
		"review":       {reviewReviseCriticalAndMinor("nil deref on empty diff", "stale comment"), reviewPass()},
		"review-fixer": {contractWithSource("internal/tdd/flow.go", "@s1")},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	setAutoFixReviewOption(t, &opts)

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review-fixer"] != 1 {
		t.Fatalf("review-fixer dispatched %d times, want 1 (AutoFixReview on restores the existing loop)", d.calls["review-fixer"])
	}
	if d.calls["review"] != 2 {
		t.Fatalf("review dispatched %d times, want 2 (initial + clean re-review)", d.calls["review"])
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if !strings.Contains(out.String(), "review complete: no blocking findings.") {
		t.Fatalf("output = %q, want the unchanged clean-resolution message %q", out.String(), "review complete: no blocking findings.")
	}
}
