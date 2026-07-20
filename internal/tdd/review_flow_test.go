package tdd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// review_flow_test.go — RED-phase tests for the post-loop whole-branch review
// stage in tdd.Run (feature review-post-loop-flow, scenarios @s1..@s9). These
// drive tdd.Run with the existing fake-dispatcher convention (no real git, no
// real agents). The review scope is injected on Options.ReviewScope so no git
// runs — mirroring how Snapshot/Diff are injected on the sandwich path.
//
// Until Options.ReviewScope and the post-loop review logic exist these fail to
// compile / assert, which is the expected RED.

// reviewFlowDispatcher returns queued replies keyed by agent name in call order
// (like seqDispatcher), and also records every task it dispatched — per agent
// (tasks) and as an ordered event log (log, when set) so tests can assert the
// relative order of review / review-fixer dispatches against test-runner calls.
type reviewFlowDispatcher struct {
	byAgent map[string][]string
	calls   map[string]int
	tasks   map[string][]string
	log     *[]string
}

func newReviewDispatcher(byAgent map[string][]string) *reviewFlowDispatcher {
	return &reviewFlowDispatcher{byAgent: byAgent, calls: map[string]int{}, tasks: map[string][]string{}}
}

func (d *reviewFlowDispatcher) Dispatch(_ context.Context, agent, task string) (string, error) {
	d.tasks[agent] = append(d.tasks[agent], task)
	if d.log != nil {
		*d.log = append(*d.log, "dispatch:"+agent)
	}
	i := d.calls[agent]
	d.calls[agent]++
	replies := d.byAgent[agent]
	if i >= len(replies) {
		return "", fmt.Errorf("reviewFlowDispatcher: no scripted reply #%d for agent %q", i, agent)
	}
	return replies[i], nil
}

// --- scripted stage replies -------------------------------------------------

var (
	rvArch      = jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)
	rvCraft     = jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)
	rvJudgePass = jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)
)

func reviewPass() string {
	return jsonBlock(`{"stage":"review","status":"pass","verdict":{"result":"pass"}}`)
}

func reviewPassMinorOnly() string {
	return jsonBlock(`{"stage":"review","status":"pass","verdict":{"result":"pass","findings":[` +
		`{"severity":"minor","location":"internal/tdd/flow.go:10","summary":"stale comment"}]}}`)
}

func reviewReviseCritical(summary string) string {
	return jsonBlock(`{"stage":"review","status":"revise","verdict":{"result":"revise","findings":[` +
		`{"severity":"critical","location":"internal/tdd/flow.go:42","summary":"` + summary + `"}]}}`)
}

// okScope is an injected review scope with a real base + changed files and no
// skip reason: the review must run.
func okScope(context.Context, string) (string, []string, string, error) {
	return "abc123", []string{"internal/tdd/flow.go"}, "", nil
}

// reviewOptions builds a standard-complexity Options wired for the post-loop
// review, mirroring baseOptions from flow_test.go but with an injected
// ReviewScope and a caller-owned Out builder.
func reviewOptions(t *testing.T, d Dispatcher, out *strings.Builder, runner TestRunner, stdin string,
	scope func(context.Context, string) (string, []string, string, error)) Options {
	t.Helper()
	return Options{
		Analyst:    passAnalyst(),
		Dispatcher: d,
		Runner:     runner,
		In:         strings.NewReader(stdin),
		Out:        out,
		Task:       "add count command",
		WorkDir:    t.TempDir(),
		FeatureReader: func(string) (string, error) {
			return "@s1\nScenario: empty\n@s2\nScenario: many\n", nil
		},
		Budget:      3,
		ReviewScope: scope,
	}
}

// loadReview reloads the persisted state.json and returns its Review field.
func loadReview(t *testing.T, workDir string) string {
	t.Helper()
	st, err := LoadState(filepath.Join(workDir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	return st.Review
}

// lastDispatch returns the agent of the most recent "dispatch:*" event.
func lastDispatch(log []string) string {
	for i := len(log) - 1; i >= 0; i-- {
		if strings.HasPrefix(log[i], "dispatch:") {
			return strings.TrimPrefix(log[i], "dispatch:")
		}
	}
	return ""
}

// idxAll returns the indices of every event equal to want.
func idxAll(log []string, want string) []int {
	var out []int
	for i, e := range log {
		if e == want {
			out = append(out, i)
		}
	}
	return out
}

// anyBetween reports whether some element of idx lies strictly between lo and hi.
func anyBetween(idx []int, lo, hi int) bool {
	for _, i := range idx {
		if i > lo && i < hi {
			return true
		}
	}
	return false
}

// @s1 — all features pass → review dispatched exactly once, run passes, state
// review recorded "pass".
func TestReviewRunsOnceWhenAllFeaturesPass(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		"review":    {reviewPass()},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["review"] != 1 {
		t.Fatalf("review dispatched %d times, want 1", d.calls["review"])
	}
	if d.calls["review-fixer"] != 0 {
		t.Fatalf("review-fixer dispatched %d times, want 0", d.calls["review-fixer"])
	}
	if got := loadReview(t, opts.WorkDir); got != "pass" {
		t.Fatalf("persisted state.Review = %q, want pass", got)
	}
}

// @s2 — a blocked feature suppresses the review entirely.
func TestReviewSkippedWhenFeatureBlocked(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		// judge "fail" blocks the feature -> the run stops before any review.
		"judge": {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"fail"}}`)},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review"] != 0 {
		t.Fatalf("review dispatched %d times, want 0 when a feature is blocked", d.calls["review"])
	}
	if res.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked", res.Status)
	}
}

// @s3 — the trivial path never reviews.
func TestReviewNeverRunsOnTrivialPath(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"trivial","handoff":"rename"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass"}`)},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass on trivial", res.Status)
	}
	if d.calls["review"] != 0 {
		t.Fatalf("trivial path dispatched review %d times, want 0", d.calls["review"])
	}
}

// @s4 — an injected review scope that reports a skip reason skips the review
// with a visible warning; the run still passes and the state records "skipped".
func TestReviewSkippedWithWarningOnScopeSkip(t *testing.T) {
	const skipReason = "no merge-base with default branch"
	skipScope := func(context.Context, string) (string, []string, string, error) {
		return "", nil, skipReason, nil
	}
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", skipScope)
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review"] != 0 {
		t.Fatalf("review dispatched %d times, want 0 when the scope reports a skip", d.calls["review"])
	}
	if !strings.Contains(out.String(), skipReason) {
		t.Fatalf("output = %q, want a visible warning naming the skip reason %q", out.String(), skipReason)
	}
	if got := loadReview(t, opts.WorkDir); got != "skipped" {
		t.Fatalf("persisted state.Review = %q, want skipped", got)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (a skipped review never fails the run)", res.Status)
	}
}

// @s5 — a review verdict "pass" carrying only minor findings dispatches no
// fixer and passes.
func TestReviewMinorFindingsAreReportOnly(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		"review":    {reviewPassMinorOnly()},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review-fixer"] != 0 {
		t.Fatalf("review-fixer dispatched %d times, want 0 for minor-only findings", d.calls["review-fixer"])
	}
	if d.calls["review"] != 1 {
		t.Fatalf("review dispatched %d times, want 1", d.calls["review"])
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
}

// @s6 — a critical finding dispatches the fixer once with the finding in its
// task; the suite is verified green after the fixer and before the re-review;
// the review is dispatched twice in total and the run passes.
func TestReviewCriticalDispatchesFixerThenReReviewPasses(t *testing.T) {
	const finding = "nil deref on empty diff"
	var log []string
	d := newReviewDispatcher(map[string][]string{
		"architect":    {rvArch},
		"craftsman":    {rvCraft},
		"judge":        {rvJudgePass},
		"review":       {reviewReviseCritical(finding), reviewPass()},
		"review-fixer": {contractWithSource("internal/tdd/flow.go", "@s1")},
	})
	d.log = &log
	runner := func(context.Context) (bool, string, error) {
		log = append(log, "runner")
		return true, "ok", nil
	}
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, runner, "approved\n", okScope)
	// This test exercises the auto-fix fixer loop; the default is off since
	// tdd.auto_fix_review, so opt in explicitly.
	opts.AutoFixReview = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["review-fixer"] != 1 {
		t.Fatalf("review-fixer dispatched %d times, want 1", d.calls["review-fixer"])
	}
	if d.calls["review"] != 2 {
		t.Fatalf("review dispatched %d times, want 2 (initial + re-review)", d.calls["review"])
	}
	if fx := d.tasks["review-fixer"]; len(fx) != 1 || !strings.Contains(fx[0], finding) {
		t.Fatalf("review-fixer task = %v, want it to carry the finding %q", fx, finding)
	}
	// The suite must be verified green AFTER the fixer and BEFORE the re-review.
	revs := idxAll(log, "dispatch:review")
	fixes := idxAll(log, "dispatch:review-fixer")
	runs := idxAll(log, "runner")
	if len(revs) != 2 || len(fixes) != 1 {
		t.Fatalf("event log dispatch counts: review=%d fixer=%d, want 2/1 (log=%v)", len(revs), len(fixes), log)
	}
	if !(fixes[0] > revs[0] && anyBetween(runs, fixes[0], revs[1])) {
		t.Fatalf("expected test runner (green) between the fixer and the re-review; log=%v", log)
	}
}

// @s7 — a fixer that breaks the suite is re-fed the failing-suite output within
// the same budget round, before any re-review is dispatched.
func TestReviewBrokenSuiteRefeedsFixerBeforeReReview(t *testing.T) {
	const suiteMarker = "SUITE_RED_MARK"
	var log []string
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		"review":    {reviewReviseCritical("guard rail missing"), reviewPass()},
		"review-fixer": {
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
		},
	})
	d.log = &log
	redOnce := true
	runner := func(context.Context) (bool, string, error) {
		log = append(log, "runner")
		// Return red exactly once, on the first verify following a fixer dispatch.
		if lastDispatch(log) == "review-fixer" && redOnce {
			redOnce = false
			return false, suiteMarker, nil
		}
		return true, "ok", nil
	}
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, runner, "approved\n", okScope)
	// This test exercises the auto-fix fixer loop; the default is off since
	// tdd.auto_fix_review, so opt in explicitly.
	opts.AutoFixReview = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	fx := d.tasks["review-fixer"]
	if len(fx) != 2 {
		t.Fatalf("review-fixer dispatched %d times, want 2 (re-fed the broken suite)", len(fx))
	}
	if !strings.Contains(fx[1], suiteMarker) {
		t.Fatalf("second fixer task = %q, want it to carry the failing-suite output %q", fx[1], suiteMarker)
	}
	// The re-feed must precede any re-review: no "dispatch:review" between the
	// two fixer dispatches.
	fixes := idxAll(log, "dispatch:review-fixer")
	if len(fixes) != 2 {
		t.Fatalf("fixer dispatch events = %d, want 2 (log=%v)", len(fixes), log)
	}
	for _, r := range idxAll(log, "dispatch:review") {
		if r > fixes[0] && r < fixes[1] {
			t.Fatalf("a re-review was dispatched before the broken suite was re-fed; log=%v", log)
		}
	}
	// Only one fixer budget round consumed: exactly one re-review after the round.
	if d.calls["review"] != 2 {
		t.Fatalf("review dispatched %d times, want 2 (only one budget round consumed)", d.calls["review"])
	}
}

// @s8 — persistent criticals through the whole fixer budget end the run
// "pass with pending findings": no third fixer, explicit message, result pass.
func TestReviewBudgetExhaustedPassesWithPendingFindings(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		"review": {
			reviewReviseCritical("still broken 1"),
			reviewReviseCritical("still broken 2"),
			reviewReviseCritical("still broken 3"),
		},
		"review-fixer": {
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
		},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", okScope)
	// This test exercises the auto-fix fixer loop; the default is off since
	// tdd.auto_fix_review, so opt in explicitly.
	opts.AutoFixReview = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d.calls["review-fixer"] != 2 {
		t.Fatalf("review-fixer dispatched %d times, want 2 (budget of 2 rounds, then stop)", d.calls["review-fixer"])
	}
	msg := strings.ToLower(out.String())
	if !strings.Contains(msg, "pending") || !strings.Contains(msg, "findings") {
		t.Fatalf("output = %q, want an explicit pass-with-pending-findings message", out.String())
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (features already passed their gates)", res.Status)
	}
}

// @s9 — a persisted state with every feature "pass" and review "pending"
// resumes straight into the review: no feature stage runs, review dispatched
// once.
func TestReviewResumeLandsOnReviewNotFeatures(t *testing.T) {
	d := newReviewDispatcher(map[string][]string{
		"review": {reviewPass()},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "resume\n", okScope)

	// Seed a resumable state: the single feature passed, review still pending.
	st, err := BeginRun("add count command", "", []FeaturePlan{{Name: "count"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	st.Mark("count", "pass")
	st.Review = "pending"
	if err := SaveState(filepath.Join(opts.WorkDir, "state.json"), st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	for _, stage := range []string{"architect", "craftsman", "judge"} {
		if d.calls[stage] != 0 {
			t.Fatalf("resume must not dispatch feature stage %q (got %d)", stage, d.calls[stage])
		}
	}
	if d.calls["review"] != 1 {
		t.Fatalf("review dispatched %d times, want 1 on resume", d.calls["review"])
	}
	if got := loadReview(t, opts.WorkDir); got != "pass" {
		t.Fatalf("persisted state.Review = %q, want pass", got)
	}
}

// @s10 (revise round, fix 2) — the review-fixer exhausts its re-feed budget with
// the suite still RED on every attempt. The flow must make the red state VISIBLE
// (an explicit suite-left-red message), must NOT re-dispatch the review after the
// failed round as if the suite were green, and must still report the run as pass
// (pending findings never block a run whose features already passed their gates).
//
// The runner is green during the feature-phase deterministic gate (last dispatch
// is the craftsman) and RED on every review-fixer verification, so the feature
// loop passes but no fixer round can ever restore green.
func TestReviewRefeedExhaustionLeavesSuiteRedVisibly(t *testing.T) {
	const suiteMarker = "SUITE_STAYS_RED"
	var log []string
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
		// Generous scripted replies: the current (unfixed) code re-reviews once
		// per round up to the round budget, so it consumes several review and
		// fixer replies. The fixed code must review exactly once. Extra replies
		// are harmless (unused) — this keeps the RED failure an ASSERTION failure,
		// not a dispatcher-exhaustion error.
		"review": {
			reviewReviseCritical("cannot be fixed 1"),
			reviewReviseCritical("cannot be fixed 2"),
			reviewReviseCritical("cannot be fixed 3"),
			reviewReviseCritical("cannot be fixed 4"),
		},
		"review-fixer": {
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
			contractWithSource("internal/tdd/flow.go", "@s1"),
		},
	})
	d.log = &log
	runner := func(context.Context) (bool, string, error) {
		log = append(log, "runner")
		if lastDispatch(log) == "review-fixer" {
			return false, suiteMarker, nil
		}
		return true, "ok", nil
	}
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, runner, "approved\n", okScope)
	// This test exercises the auto-fix fixer loop; the default is off since
	// tdd.auto_fix_review, so opt in explicitly.
	opts.AutoFixReview = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Never blocks: the features already passed their gates.
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (a red review round never blocks the run)", res.Status)
	}
	// The fixer must have been given at least one chance.
	if d.calls["review-fixer"] == 0 {
		t.Fatalf("review-fixer dispatched 0 times, want at least one attempt")
	}
	// The review must NOT be re-dispatched after the fixer gave up with a red
	// suite — a re-review would proceed as if the suite were green.
	if d.calls["review"] != 1 {
		t.Fatalf("review dispatched %d times, want exactly 1 (no re-review after the fixer left the suite red)", d.calls["review"])
	}
	// The red state must be VISIBLE: an explicit suite-left-red message
	// (asserts case-insensitive substrings "suite" and "red").
	msg := strings.ToLower(out.String())
	if !strings.Contains(msg, "suite") || !strings.Contains(msg, "red") {
		t.Fatalf("output = %q, want an explicit message that the suite was left red", out.String())
	}
	// The closing message must NOT falsely claim a clean state.
	if strings.Contains(msg, "no blocking findings") {
		t.Fatalf("output = %q, must not claim 'no blocking findings' while the suite is red", out.String())
	}
}

// @s11 (revise round, fix 3) — the review scope fails with an ERROR (not a skip
// reason) after every feature passed. Review must already be persisted "pending"
// BEFORE the scope is computed, so a transient scope failure resumes straight
// back into the review on the next run instead of forfeiting the whole completed
// feature loop. Per spec section B, scope problems are non-fatal: a visible
// warning naming the failure, and the run still passes.
func TestReviewScopeErrorPersistsPendingAndWarns(t *testing.T) {
	const scopeErr = "git rev-parse HEAD~: exit status 128"
	failScope := func(context.Context, string) (string, []string, string, error) {
		return "", nil, "", errors.New(scopeErr)
	}
	d := newReviewDispatcher(map[string][]string{
		"architect": {rvArch},
		"craftsman": {rvCraft},
		"judge":     {rvJudgePass},
	})
	out := &strings.Builder{}
	opts := reviewOptions(t, d, out, green, "approved\n", failScope)
	res, err := Run(context.Background(), opts)
	// A scope failure must be non-fatal: a warning, not a returned error.
	if err != nil {
		t.Fatalf("run returned error %v, want nil — a scope failure must surface as a visible warning, never fail the run", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (a scope failure never blocks a run whose features passed)", res.Status)
	}
	// There was no scope to review, so the review is not dispatched.
	if d.calls["review"] != 0 {
		t.Fatalf("review dispatched %d times, want 0 when the scope failed", d.calls["review"])
	}
	// Review must already be persisted "pending" so the next run resumes into the
	// review rather than restarting the completed feature loop.
	if got := loadReview(t, opts.WorkDir); got != "pending" {
		t.Fatalf("persisted state.Review = %q, want pending (must be persisted BEFORE the scope call)", got)
	}
	// The scope failure must be named in a visible warning.
	if !strings.Contains(out.String(), scopeErr) {
		t.Fatalf("output = %q, want a visible warning naming the scope failure %q", out.String(), scopeErr)
	}
}
