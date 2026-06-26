package tdd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunResumeSkipsDoneFeatures(t *testing.T) {
	opts := baseOptions(t, &seqDispatcher{calls: map[string]int{}, byAgent: map[string][]string{
		// Only the second feature should run; analyst/architect must NOT be called.
		"craftsman": {jsonBlock(`{"stage":"craftsman","status":"pass","scenarios":["@s1","@s2"]}`)},
		"judge":     {jsonBlock(`{"stage":"judge","status":"pass","verdict":{"result":"pass"}}`)},
	}}, &countingChatter{}, green, "resume\n")
	// Seed a resumable state: f1 done, f2 pending.
	st := BeginRun("t", "", []string{"f1", "f2"})
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
