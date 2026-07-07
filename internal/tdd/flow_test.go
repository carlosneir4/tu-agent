package tdd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/testresult"
)

// seqDispatcher returns replies keyed by agent name, in call order per agent.
type seqDispatcher struct {
	byAgent map[string][]string
	calls   map[string]int
}

func (s *seqDispatcher) Dispatch(_ context.Context, agent, _ string) (string, error) {
	i := s.calls[agent]
	s.calls[agent]++
	return s.byAgent[agent][i], nil
}

func jsonBlock(s string) string { return "ok\n```json\n" + s + "\n```" }

func baseOptions(t *testing.T, d *seqDispatcher, analyst Chatter, runner TestRunner, stdin string) Options {
	return Options{
		Analyst:    analyst,
		Dispatcher: d,
		Runner:     runner,
		In:         strings.NewReader(stdin),
		Out:        &strings.Builder{},
		Task:       "add count command",
		WorkDir:    t.TempDir(),
		FeatureReader: func(string) (string, error) {
			return "@s1\nScenario: empty\n@s2\nScenario: many\n", nil
		},
		Budget: 3,
	}
}

func passAnalyst() Chatter {
	return &scriptChatter{replies: []string{jsonBlock(`{"stage":"analyst","status":"pass"}`)}}
}

func green(context.Context) (bool, string, error) { return true, "ok", nil }

type countingChatter struct{ calls int }

func (c *countingChatter) Chat(_ context.Context, _ string) (string, error) {
	c.calls++
	return jsonBlock(`{"stage":"analyst","status":"pass"}`), nil
}

// errStopAnalyst is the sentinel errChatter returns to make Run stop right
// after dispatching the analyst's first turn — enough to inspect the task
// it was given without a live LLM or a full contract exchange.
var errStopAnalyst = errors.New("errChatter: stop after analyst turn")

// errChatter records the input (the analyst task) it received, then errors so
// RunAnalyst — and therefore Run — returns immediately.
type errChatter struct{ got string }

func (e *errChatter) Chat(_ context.Context, input string) (string, error) {
	e.got = input
	return "", errStopAnalyst
}

func TestRunHappyPath(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
}

func TestRunReviseThenPass(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`),
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`),
		},
		"judge": {
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"revise","feedback":"rename x"}}`),
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`),
		},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass after revise", res.Status)
	}
}

func TestRunBudgetExhausted(t *testing.T) {
	revise := jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"revise","feedback":"again"}}`)
	craft := jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {craft, craft, craft},
		"judge":     {revise, revise, revise},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked", res.Status)
	}
	if d.calls["craftsman"] != 3 {
		t.Fatalf("craftsman dispatched %d times, want 3", d.calls["craftsman"])
	}
}

func TestRunDeterministicGateFailsThenPasses(t *testing.T) {
	// First craftsman reply covers only @s1 — det-gate fails, budget consumed, no judge call.
	// Second craftsman reply covers @s1+@s2 — det-gate passes, judge runs once and passes.
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1"]}`),
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`),
		},
		"judge": {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["judge"] != 1 {
		t.Fatalf("judge called %d times, want 1", d.calls["judge"])
	}
	if d.calls["craftsman"] != 2 {
		t.Fatalf("craftsman called %d times, want 2", d.calls["craftsman"])
	}
}

func TestRunHumanGateRejected(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "stop\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked on rejection", res.Status)
	}
}

func TestRunTrivialBypass(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"trivial","handoff":"rename"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass"}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass on trivial", res.Status)
	}
	if d.calls["judge"] != 0 {
		t.Fatalf("trivial path must not call the judge")
	}
}

func TestRunMutationGateBlocksThenPasses(t *testing.T) {
	craftWithSrc := jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"],"artifacts":[{"kind":"source","path":"internal/x/x.go"}]}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {craftWithSrc, craftWithSrc},
		"judge": {
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`),
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`),
		},
	}}
	calls := 0
	mut := func(_ context.Context, _ MutationTarget) MutationOutcome {
		calls++
		if calls == 1 {
			return MutationOutcome{Score: 0.2, Survivors: []string{"x.go:10 if->true"}}
		}
		return MutationOutcome{Score: 0.9}
	}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Mutator = mut
	opts.MutationThreshold = 0.7
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["craftsman"] != 2 {
		t.Fatalf("craftsman dispatched %d times, want 2 (gate sent it back once)", d.calls["craftsman"])
	}
}

func TestRunMutationGatePassesFirstTry(t *testing.T) {
	craftWithSrc := jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"],"artifacts":[{"kind":"source","path":"internal/x/x.go"}]}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {craftWithSrc},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	mut := func(_ context.Context, _ MutationTarget) MutationOutcome {
		return MutationOutcome{Score: 0.9}
	}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Mutator = mut
	opts.MutationThreshold = 0.7
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["craftsman"] != 1 {
		t.Fatalf("craftsman dispatched %d times, want 1", d.calls["craftsman"])
	}
}

func TestRunMutationSkippedIsAdvisory(t *testing.T) {
	craftWithSrc := jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"],"artifacts":[{"kind":"source","path":"internal/x/x.go"}]}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {craftWithSrc},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	mut := func(_ context.Context, _ MutationTarget) MutationOutcome {
		return MutationOutcome{Skipped: true, Note: "tool absent"}
	}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Mutator = mut
	opts.MutationThreshold = 0.7
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (skip is advisory)", res.Status)
	}
	if d.calls["craftsman"] != 1 {
		t.Fatalf("craftsman dispatched %d times, want 1", d.calls["craftsman"])
	}
}

func TestRunArchivesOnStandardSuccess(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
		"scribe":    {jsonBlock(`{"stage":"scribe","status":"pass"}`)},
	}}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Archive = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["scribe"] != 1 {
		t.Fatalf("scribe dispatched %d times, want 1", d.calls["scribe"])
	}
}

func TestRunArchiveDisabled(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Archive = false
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if d.calls["scribe"] != 0 {
		t.Fatalf("scribe dispatched %d times, want 0", d.calls["scribe"])
	}
}

func TestRunArchiveBestEffort(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
		"scribe":    {"not a valid contract"}, // ParseContract fails -> runner.Run errors
	}}
	opts := baseOptions(t, d, passAnalyst(), green, "approved\n")
	opts.Archive = true
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass (archive is best-effort)", res.Status)
	}
	if d.calls["scribe"] != 1 {
		t.Fatalf("scribe dispatched %d times, want 1", d.calls["scribe"])
	}
}

func TestRunSharedStdinReader(t *testing.T) {
	// The analyst asks ONE question (consuming one stdin line) before it passes.
	// With split readers, the analyst's bufio.Reader buffers past "answer1\n" and
	// swallows "approved\n", so the human gate reads EOF and blocks. A single
	// shared reader preserves the approval line.
	analyst := &scriptChatter{replies: []string{
		"What output format?",
		jsonBlock(`{"stage":"analyst","status":"pass"}`),
	}}
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"trivial","handoff":"rename"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass"}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, analyst, green, "answer1\napproved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass — the approval line was lost to the analyst's reader", res.Status)
	}
}

func TestRunMultiFeatureComplex(t *testing.T) {
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"complex","features":[{"name":"f1","scenarios":["@s1","@s2"]},{"name":"f2","scenarios":["@s1","@s2"]}]}`)},
		"craftsman": {
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`),
			jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`),
		},
		"judge": {
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`),
			jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`),
		},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "approved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	if len(res.Features) != 2 || res.Features[0].Status != "pass" || res.Features[1].Status != "pass" {
		t.Fatalf("features = %+v", res.Features)
	}
	if d.calls["craftsman"] != 2 || d.calls["judge"] != 2 {
		t.Fatalf("each feature runs the loop once: craftsman=%d judge=%d", d.calls["craftsman"], d.calls["judge"])
	}
}

func TestRunHumanGateReviseThenApprove(t *testing.T) {
	arch := jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {arch, arch},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "split it differently\napproved\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass after revise", res.Status)
	}
	if d.calls["architect"] != 2 {
		t.Fatalf("architect dispatched %d times, want 2 (revise then approve)", d.calls["architect"])
	}
}

func TestRunDesignBudgetExhausted(t *testing.T) {
	arch := jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)
	d := &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {arch, arch, arch},
	}}
	res, err := Run(context.Background(), baseOptions(t, d, passAnalyst(), green, "a\nb\nc\n"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked after budget", res.Status)
	}
	if d.calls["architect"] != 3 {
		t.Fatalf("architect dispatched %d times, want 3", d.calls["architect"])
	}
}

// TestRunFeatureTDDRefactor proves kind:"refactor" skips RED entirely: the
// scripted dispatcher provides only the refactor + judge dispatches (a
// wrongly-run RED phase would exhaust the fixture and panic), the whole suite
// is green from the first check, and the resulting scenarios/judge task carry
// the refactor marking.
func TestRunFeatureTDDRefactor(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // refactor dispatch only — NOT test-writer/implementer
		// judge:
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	o := Options{
		Dispatcher:  disp,
		Runner:      func(context.Context) (bool, string, error) { return true, "", nil }, // suite green throughout
		Out:         &bytes.Buffer{},
		Budget:      2,
		Snapshot:    func(context.Context) (string, error) { return "S", nil },
		Diff:        func(context.Context, string, string) ([]string, error) { return nil, nil },
		LoadReports: func(time.Time) (testresult.Report, error) { return testresult.Report{}, nil },
		// A refactor feature must never invoke the mutator — no new tests to harden.
		Mutator: func(context.Context, MutationTarget) MutationOutcome {
			t.Fatal("mutation gate must not run for a refactor feature")
			return MutationOutcome{}
		},
	}
	runner := StageRunner{D: disp}
	status, scenarios, err := runFeatureTDD(context.Background(), o, runner, "feat", "refactor", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
	if len(scenarios) != 1 || scenarios[0].Tag != "@s1" || scenarios[0].Kind != "refactor" || scenarios[0].Phase != "done" {
		t.Fatalf("scenarios = %+v, want [{Tag:@s1 Phase:done Kind:refactor}]", scenarios)
	}
	var judgeTask string
	for _, task := range disp.tasks {
		if strings.HasPrefix(task, "judge|") {
			judgeTask = task
			break
		}
	}
	if !strings.Contains(judgeTask, "this is a refactor") {
		t.Fatalf("judge task = %q, want it to note this is a refactor", judgeTask)
	}
	if len(disp.tasks) != 2 {
		t.Fatalf("expected exactly 2 dispatches (refactor, judge), got %d: %v", len(disp.tasks), disp.tasks)
	}
}

func TestRunFeatureTDDUsesSandwich(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer
		contract("@s1"), // implementer
		// judge:
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	snaps := []string{"S0", "S1", "S2"}
	var si, runN int
	o := Options{
		Dispatcher: disp,
		Runner:     func(context.Context) (bool, string, error) { runN++; return runN >= 2, "", nil },
		Out:        &bytes.Buffer{},
		Budget:     2,
		Snapshot:   func(context.Context) (string, error) { s := snaps[si%len(snaps)]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" {
				return []string{"src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail}}}, nil
		},
	}
	runner := StageRunner{D: disp}
	status, _, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
}

// TestRunFeatureTDDRecordsRegression proves a green-on-arrival sandwich cycle
// is threaded through to the per-scenario ScenarioState (Kind "regression",
// Phase "done") AND that the judge dispatch task carries a note telling it
// not to credit those scenarios as RED->GREEN TDD victories.
func TestRunFeatureTDDRecordsRegression(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer only — green-on-arrival skips the GREEN phase
		// judge:
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	snaps := []string{"S0", "S1"}
	var si int
	o := Options{
		Dispatcher: disp,
		Runner:     func(context.Context) (bool, string, error) { return false, "", nil }, // suite red overall
		Out:        &bytes.Buffer{},
		Budget:     2,
		Snapshot:   func(context.Context) (string, error) { s := snaps[si%len(snaps)]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"src/test/java/com/acme/FooTest.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass}}}, nil
		},
	}
	runner := StageRunner{D: disp}
	status, scenarios, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
	if len(scenarios) != 1 || scenarios[0].Tag != "@s1" || scenarios[0].Kind != "regression" || scenarios[0].Phase != "done" {
		t.Fatalf("scenarios = %+v, want [{Tag:@s1 Phase:done Kind:regression}]", scenarios)
	}
	var judgeTask string
	for _, task := range disp.tasks {
		if strings.HasPrefix(task, "judge|") {
			judgeTask = task
			break
		}
	}
	if !strings.Contains(judgeTask, "green-on-arrival regression") {
		t.Fatalf("judge task = %q, want it to note the green-on-arrival regression", judgeTask)
	}
}

// TestRunFeatureTDDNormalPassRecordsTDDKind proves a normal RED->GREEN
// sandwich cycle records ScenarioState.Kind "tdd" (not "regression") and does
// NOT add a green-on-arrival note to the judge dispatch task.
func TestRunFeatureTDDNormalPassRecordsTDDKind(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer
		contract("@s1"), // implementer
		// judge:
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	snaps := []string{"S0", "S1", "S2"}
	var si, runN int
	o := Options{
		Dispatcher: disp,
		Runner:     func(context.Context) (bool, string, error) { runN++; return runN >= 2, "", nil },
		Out:        &bytes.Buffer{},
		Budget:     2,
		Snapshot:   func(context.Context) (string, error) { s := snaps[si%len(snaps)]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" {
				return []string{"src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail}}}, nil
		},
	}
	runner := StageRunner{D: disp}
	status, scenarios, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
	if len(scenarios) != 1 || scenarios[0].Tag != "@s1" || scenarios[0].Kind != "tdd" || scenarios[0].Phase != "done" {
		t.Fatalf("scenarios = %+v, want [{Tag:@s1 Phase:done Kind:tdd}]", scenarios)
	}
	var judgeTask string
	for _, task := range disp.tasks {
		if strings.HasPrefix(task, "judge|") {
			judgeTask = task
			break
		}
	}
	if strings.Contains(judgeTask, "green-on-arrival regression") {
		t.Fatalf("judge task = %q, must not mention green-on-arrival regression for a normal RED->GREEN pass", judgeTask)
	}
}

// TestRunFeatureTDDSandwichRunsMutation proves Task 14 re-enables the
// mutation gate on the sandwich path: it must run against the mutation
// target derived from the implementer's SandwichResult.SourceArtifact (there
// is no craftsman Contract on this path), and a passing score lets the
// feature proceed to StatusPass.
func TestRunFeatureTDDSandwichRunsMutation(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer
		contractWithSource("src/main/java/com/acme/Foo.java", "@s1"), // implementer
		// judge:
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	snaps := []string{"S0", "S1", "S2"}
	var si, runN, mutCalls int
	var gotTarget MutationTarget
	o := Options{
		Dispatcher: disp,
		Runner:     func(context.Context) (bool, string, error) { runN++; return runN >= 2, "", nil },
		Out:        &bytes.Buffer{},
		Budget:     2,
		Snapshot:   func(context.Context) (string, error) { s := snaps[si%len(snaps)]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" {
				return []string{"src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail}}}, nil
		},
		Mutator: func(_ context.Context, mt MutationTarget) MutationOutcome {
			mutCalls++
			gotTarget = mt
			return MutationOutcome{Score: 1}
		},
		MutationThreshold: 1,
	}
	runner := StageRunner{D: disp}
	status, _, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
	if mutCalls != 1 {
		t.Fatalf("mutator called %d times, want 1", mutCalls)
	}
	if gotTarget.Language != "java" || gotTarget.Dir != "src/main/java/com/acme" {
		t.Fatalf("mutation target = %+v, want {java src/main/java/com/acme}", gotTarget)
	}
}

// TestRunFeatureTDDSandwichMutationBlocks proves a failing mutation score on
// the sandwich path is a retry (feedback + continue), not terminal: the next
// iteration re-enters via RefineSandwich — matching the non-sandwich retry
// semantics — and once the budget is exhausted the feature ends blocked with
// the mutation feedback surfaced in Out.
func TestRunFeatureTDDSandwichMutationBlocks(t *testing.T) {
	src := contractWithSource("src/main/java/com/acme/Foo.java", "@s1")
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer (cycle 1, RED)
		src,             // implementer (cycle 1, GREEN)
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
		src, // implementer (refine, cycle 2)
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	snaps := []string{"S0", "S1", "S2"}
	var si, runN, mutCalls int
	out := &bytes.Buffer{}
	o := Options{
		Dispatcher: disp,
		Runner:     func(context.Context) (bool, string, error) { runN++; return runN >= 2, "", nil },
		Out:        out,
		Budget:     2,
		Snapshot:   func(context.Context) (string, error) { s := snaps[si%len(snaps)]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" {
				return []string{"src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail}}}, nil
		},
		Mutator: func(context.Context, MutationTarget) MutationOutcome {
			mutCalls++
			return MutationOutcome{Score: 0.1, Survivors: []string{"Foo.java:5 if->true"}}
		},
		MutationThreshold: 0.7,
	}
	runner := StageRunner{D: disp}
	status, _, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusBlocked {
		t.Fatalf("status=%s err=%v, want blocked/nil", status, err)
	}
	if mutCalls != 2 {
		t.Fatalf("mutator called %d times, want 2 (once per judge-pass verdict, budget 2)", mutCalls)
	}
	if !strings.Contains(out.String(), "mutation score") {
		t.Fatalf("Out = %q, want mutation feedback surfaced", out.String())
	}
}

// TestRunFeatureTDDReviseThenRefine proves that a judge "revise" on the
// sandwich path re-enters via RefineSandwich (implementer only) rather than
// re-running the whole RunSandwich cycle (which would re-dispatch the
// test-writer against tests that already exist). The dispatcher and the
// snapshot fixtures are sized to exactly the REFINE-path call counts — a
// wrongly re-run RED phase would exhaust them and panic, failing the test.
func TestRunFeatureTDDReviseThenRefine(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer (pass 1, RED)
		contract("@s1"), // implementer (pass 1, GREEN)
		// judge: revise
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"revise\",\"feedback\":\"tighten naming\"}}\n```",
		contract("@s1"), // refine implementer (pass 2) — NOT a test-writer dispatch
		// judge: pass
		"done\n```json\n{\"stage\":\"judge\",\"status\":\"pass\",\"verdict\":{\"result\":\"pass\",\"feedback\":\"ok\",\"score\":9}}\n```",
	}}
	// Snapshots: pass 1 (RunSandwich) needs 4 — before, after-tests(RED loop),
	// fresh GREEN baseline, after-impl. Pass 2 (RefineSandwich) needs 2 —
	// before, after. Sized to exactly 6; a 7th call panics.
	snaps := []string{"S0", "S1", "S1b", "S2", "S3", "S4"}
	var si int
	var runN int
	o := Options{
		Dispatcher: disp,
		// First Tests() call (RED check) is red; every call after is green
		// (pass 1's GREEN check, and pass 2's refine DeterministicJudge check).
		Runner: func(context.Context) (bool, string, error) { runN++; return runN >= 2, "", nil },
		Out:    &bytes.Buffer{},
		Budget: 3,
		Snapshot: func(context.Context) (string, error) {
			s := snaps[si] // direct index: exhausting it proves an extra RED phase ran
			si++
			return s, nil
		},
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" {
				return []string{"src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail}}}, nil
		},
	}
	runner := StageRunner{D: disp}
	status, _, err := runFeatureTDD(context.Background(), o, runner, "feat", "", []string{"@s1"})
	if err != nil || status != StatusPass {
		t.Fatalf("status=%s err=%v, want pass/nil", status, err)
	}
	if len(disp.tasks) != 5 {
		t.Fatalf("expected exactly 5 dispatches (test-writer, implementer, judge, refine-implementer, judge), got %d: %v", len(disp.tasks), disp.tasks)
	}
	if !strings.Contains(disp.tasks[3], "tighten naming") {
		t.Errorf("dispatch 3 (refine) should carry the judge's revise feedback: %q", disp.tasks[3])
	}
	if !strings.Contains(disp.tasks[3], ImplementerPrompt) {
		t.Errorf("dispatch 3 (refine) should be the implementer, not the test-writer: %q", disp.tasks[3][:min(60, len(disp.tasks[3]))])
	}
}

func TestRunResumeSkipsDoneFeatures(t *testing.T) {
	opts := baseOptions(t, &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		// Only the second feature should run; analyst/architect must NOT be called.
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}, &countingChatter{}, green, "resume\n")
	// Seed a resumable state: f1 done, f2 pending.
	st, err := BeginRun("t", "", []FeaturePlan{{Name: "f1"}, {Name: "f2"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	st.Mark("f1", "pass")
	if err := SaveState(filepath.Join(opts.WorkDir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	dd := opts.Dispatcher.(*seqDispatcher)
	if dd.calls["architect"] != 0 {
		t.Fatalf("resume must not re-run the architect")
	}
	if dd.calls["craftsman"] != 1 {
		t.Fatalf("resume should run only the one pending feature, craftsman=%d", dd.calls["craftsman"])
	}
}

// TestRunInjectsDesignSeed proves Options.DesignDoc, when set, is folded into
// the analyst task as a seed-from-design instruction, and left untouched
// (task passed through as-is) when DesignDoc is empty. The errChatter fake
// errors out on the analyst's first turn so Run returns immediately after
// building the task — no design/craftsman/judge stages need scripting.
// taskRecordingDispatcher wraps seqDispatcher but also records the first task
// string dispatched to each agent, so tests can assert on the exact text
// built for a stage (e.g. the architect's per-feature spec path) without
// needing a full contract exchange to inspect it after the fact.
type taskRecordingDispatcher struct {
	seqDispatcher
	tasks map[string]string
}

func (d *taskRecordingDispatcher) Dispatch(ctx context.Context, agent, task string) (string, error) {
	if d.tasks == nil {
		d.tasks = map[string]string{}
	}
	if _, ok := d.tasks[agent]; !ok {
		d.tasks[agent] = task
	}
	return d.seqDispatcher.Dispatch(ctx, agent, task)
}

// TestRunArchitectTaskUsesRelBase proves that when Options.RelBase is set to
// the per-feature artifact dir, the architect is dispatched a task pointing
// at that dir's spec.md — matching the path the architect's system overlay
// (WithBaseDir) already references. Before the fix this was hardcoded to the
// flat ".tu-agent/tdd/spec.md", contradicting the overlay.
func TestRunArchitectTaskUsesRelBase(t *testing.T) {
	d := &taskRecordingDispatcher{seqDispatcher: seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}}
	opts := baseOptions(t, &d.seqDispatcher, passAnalyst(), green, "approved\n")
	opts.Dispatcher = d
	opts.RelBase = ".tu-agent/tdd/ABC-1-x"
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	want := ".tu-agent/tdd/ABC-1-x/spec.md"
	if !strings.Contains(d.tasks["architect"], want) {
		t.Fatalf("architect task = %q, want it to contain %q", d.tasks["architect"], want)
	}
}

// TestRunArchitectTaskFallsBackWithoutRelBase proves that when RelBase is
// unset, the architect task falls back to the flat ".tu-agent/tdd/spec.md"
// path rather than producing a malformed path.
func TestRunArchitectTaskFallsBackWithoutRelBase(t *testing.T) {
	d := &taskRecordingDispatcher{seqDispatcher: seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		"architect": {jsonBlock(`{"stage":"architect","status":"pass","complexity":"standard","handoff":"count"}`)},
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}}
	opts := baseOptions(t, &d.seqDispatcher, passAnalyst(), green, "approved\n")
	opts.Dispatcher = d
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusPass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
	want := ".tu-agent/tdd/spec.md"
	if !strings.Contains(d.tasks["architect"], want) {
		t.Fatalf("architect task = %q, want fallback to contain %q", d.tasks["architect"], want)
	}
}

func TestRunInjectsDesignSeed(t *testing.T) {
	analyst := &errChatter{}
	o := Options{
		Analyst:   analyst,
		In:        strings.NewReader(""),
		Out:       &strings.Builder{},
		Task:      "build the thing",
		DesignDoc: "docs/superpowers/plans/x.md",
		WorkDir:   t.TempDir(),
	}
	_, _ = Run(context.Background(), o)
	if !strings.Contains(analyst.got, "docs/superpowers/plans/x.md") || !strings.Contains(analyst.got, "seed") {
		t.Fatalf("analyst task missing design seed: %q", analyst.got)
	}

	analyst.got = ""
	o.DesignDoc = ""
	_, _ = Run(context.Background(), o)
	if strings.Contains(analyst.got, "seed the spec") {
		t.Fatalf("unexpected seed instruction with no DesignDoc: %q", analyst.got)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestRunWarnsOnCorruptState guards flow.go:65 — a corrupt (not merely
// missing) state.json must not fail silently. Run should warn on stderr and
// still proceed as a fresh run instead of getting stuck.
func TestRunWarnsOnCorruptState(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "state.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	analyst := &errChatter{}
	o := Options{
		Analyst: analyst,
		In:      strings.NewReader(""),
		Out:     &strings.Builder{},
		Task:    "build the thing",
		WorkDir: workDir,
	}
	stderr := captureStderr(t, func() {
		_, _ = Run(context.Background(), o)
	})
	if !strings.Contains(stderr, "warning: tdd state unreadable") {
		t.Errorf("expected a corrupt-state warning on stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "starting fresh") {
		t.Errorf("warning should say it is starting fresh, got %q", stderr)
	}
}

// TestRunNoWarningOnMissingState guards the other side: a plain missing
// state.json (the normal case for a first run) is not an error and must not
// print the corrupt-state warning.
func TestRunNoWarningOnMissingState(t *testing.T) {
	analyst := &errChatter{}
	o := Options{
		Analyst: analyst,
		In:      strings.NewReader(""),
		Out:     &strings.Builder{},
		Task:    "build the thing",
		WorkDir: t.TempDir(),
	}
	stderr := captureStderr(t, func() {
		_, _ = Run(context.Background(), o)
	})
	if strings.Contains(stderr, "warning: tdd state unreadable") {
		t.Errorf("missing state.json must not trigger the corrupt-state warning, got %q", stderr)
	}
}
