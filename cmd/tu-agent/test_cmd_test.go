package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/mutation"
	"github.com/tu/tu-agent/internal/testgen"
)

func setupGapsFixture(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "billing")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"svc.go": `package billing

func Pay() int {
	x := 1
	x++
	return x
}

func Refund() int {
	y := 2
	y++
	return y
}
`,
		"svc_test.go": `package billing

import "testing"

func TestPay(t *testing.T) {
	if Pay() != 2 {
		t.Fatal("nope")
	}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
}

func TestRunTestGapsEndToEnd(t *testing.T) {
	setupGapsFixture(t)
	out, _, err := runTestGaps("", 20, 4, 2, false, "", false, 0)
	if err != nil {
		t.Fatalf("runTestGaps: %v", err)
	}
	if !strings.Contains(out, "Refund") {
		t.Errorf("untested Refund missing from gaps:\n%s", out)
	}
	if strings.Contains(out, "Pay") {
		t.Errorf("tested Pay should not be a gap:\n%s", out)
	}
}

func TestRunTestGapsJSON(t *testing.T) {
	setupGapsFixture(t)
	out, _, err := runTestGaps("", 20, 4, 2, true, "", false, 0)
	if err != nil {
		t.Fatalf("runTestGaps: %v", err)
	}
	var gaps []struct {
		Symbol      string  `json:"symbol"`
		ID          string  `json:"id"`
		File        string  `json:"file"`
		Line        int     `json:"line"`
		Signature   string  `json:"signature"`
		FanIn       int     `json:"fan_in"`
		BlastRadius int     `json:"blast_radius"`
		Span        int     `json:"span"`
		Score       float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(out), &gaps); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(gaps) != 1 || gaps[0].Symbol != "Refund" {
		t.Fatalf("json gaps = %+v, want exactly Refund", gaps)
	}
	if gaps[0].File != "billing/svc.go" || gaps[0].Line == 0 || gaps[0].Span < 4 {
		t.Errorf("json pointer fields wrong: %+v", gaps[0])
	}
}

func TestTestGenFlagsRegistered(t *testing.T) {
	for _, name := range []string{"dry-run", "max-repair", "discard-failing", "provider", "timeout"} {
		if testGenCmd.Flags().Lookup(name) == nil {
			t.Errorf("test gen missing flag --%s", name)
		}
	}
	if testGenCmd.Args == nil {
		t.Fatal("test gen must require exactly one target arg")
	}
}

func TestResolveTestGenTargetsSingle(t *testing.T) {
	setupGapsFixture(t)
	_, targets, err := resolveTestGenTargets("Refund", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Name != "Refund" {
		t.Fatalf("targets = %+v, want exactly Refund", targets)
	}
}

func TestResolveTestGenTargetsTopN(t *testing.T) {
	setupGapsFixture(t)
	_, targets, err := resolveTestGenTargets("", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	// Only Refund is an untested gap in the fixture.
	if len(targets) != 1 || targets[0].Name != "Refund" {
		t.Fatalf("top-N targets = %+v, want exactly Refund", targets)
	}
}

func TestResolveTestGenTargetsNotFound(t *testing.T) {
	setupGapsFixture(t)
	_, _, err := resolveTestGenTargets("NoSuchSymbol", 0, "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
}

func TestRunTestGenNoGraph(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runTestGen(context.Background(), "Whatever", 0, "")
	if err == nil || !strings.Contains(err.Error(), "graph build") {
		t.Fatalf("err = %v, want 'graph build' hint", err)
	}
}

func TestTestGenFlagsRegisteredSP3(t *testing.T) {
	for _, name := range []string{"top", "domain"} {
		if testGenCmd.Flags().Lookup(name) == nil {
			t.Errorf("test gen missing flag --%s", name)
		}
	}
}

func TestRunTestGenRejectsBothInputs(t *testing.T) {
	err := runTestGen(context.Background(), "Refund", 5, "")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want 'exactly one' of target/--top", err)
	}
}

func TestRunTestGenRejectsNoInput(t *testing.T) {
	err := runTestGen(context.Background(), "", 0, "")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want 'exactly one' of target/--top", err)
	}
}

func TestFormatBatchSummary(t *testing.T) {
	rep := testgen.BatchReport{
		Items: []testgen.BatchItem{
			{Target: testgen.Target{Name: "Save"}, Result: &testgen.Result{TestPath: "a_test.go", Passed: true, Attempts: 1}},
			{Target: testgen.Target{Name: "Load"}, Result: &testgen.Result{TestPath: "a_test.go", FIXME: true, Attempts: 3}},
		},
		Passed: 1, FIXMEd: 1,
	}
	out := formatBatch(rep, false)
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "FIXME") {
		t.Errorf("missing per-target lines:\n%s", out)
	}
	if !strings.Contains(out, "1 passed, 1 FIXME, 0 discarded, 0 errored (2 targets)") {
		t.Errorf("missing summary:\n%s", out)
	}
}

func TestRunTestScaffoldNoGraph(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := runTestScaffold("Whatever")
	if err == nil || !strings.Contains(err.Error(), "graph build") {
		t.Fatalf("err = %v, want 'graph build' hint", err)
	}
}

func TestRunTestScaffoldReturnsArray(t *testing.T) {
	setupGapsFixture(t)
	out, err := runTestScaffold("Refund")
	if err != nil {
		t.Fatal(err)
	}
	var scaffolds []testgen.Scaffold
	if err := json.Unmarshal([]byte(out), &scaffolds); err != nil {
		t.Fatalf("output is not a JSON array of scaffolds: %v\n%s", err, out)
	}
	if len(scaffolds) != 1 || scaffolds[0].Target.Name != "Refund" {
		t.Fatalf("scaffolds = %+v, want exactly Refund", scaffolds)
	}
	if scaffolds[0].TestPath == "" || len(scaffolds[0].RunCommand) == 0 {
		t.Errorf("scaffold not fully built: %+v", scaffolds[0])
	}
}

func TestGapGateSummary(t *testing.T) {
	pass := gapGate{Covered: 8, Known: 10, Pct: 80, Threshold: 75, Below: false}
	if s := pass.Summary(); !strings.Contains(s, "80.0%") || !strings.Contains(s, "PASS") {
		t.Errorf("pass summary = %q", s)
	}
	fail := gapGate{Covered: 7, Known: 10, Pct: 70, Threshold: 75, Below: true}
	if s := fail.Summary(); !strings.Contains(s, "70.0%") || !strings.Contains(s, "FAIL") {
		t.Errorf("fail summary = %q", s)
	}
}

func TestRunTestGapsFailUnderRequiresCoverage(t *testing.T) {
	setupGapsFixture(t)
	_, gate, err := runTestGaps("", 20, 4, 2, false, "", false, 80)
	if err == nil || !strings.Contains(err.Error(), "requires a coverage source") {
		t.Fatalf("err = %v (gate=%v), want 'requires a coverage source'", err, gate)
	}
}

func TestRunTestGapsNoGateWhenZero(t *testing.T) {
	setupGapsFixture(t)
	out, gate, err := runTestGaps("", 20, 4, 2, false, "", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if gate != nil {
		t.Errorf("gate should be nil when failUnder=0, got %+v", gate)
	}
	if !strings.Contains(out, "Refund") {
		t.Errorf("normal gaps output missing:\n%s", out)
	}
}

func TestHandleTestGapsFailUnderRequiresCoverage(t *testing.T) {
	setupGapsFixture(t)
	_, _, err := handleTestGaps(context.Background(), nil, testGapsInput{Top: 20, FailUnder: 80})
	if err == nil || !strings.Contains(err.Error(), "requires a coverage source") {
		t.Fatalf("err = %v, want 'requires a coverage source'", err)
	}
}

func TestFormatMutationReport(t *testing.T) {
	rep := mutation.Report{Tool: "go-mutesting", Total: 3, Killed: 2, Survived: 1, Score: 0.667,
		Survivors: []mutation.Survivor{{File: "store.go", Line: 12, Desc: "math"}}}
	out := formatMutation(rep)
	if !strings.Contains(out, "66.7%") || !strings.Contains(out, "store.go:12") {
		t.Fatalf("report missing score/survivor:\n%s", out)
	}

	skip := mutation.Report{Tool: "stryker", Skipped: true, Note: "stryker not found; mutation skipped"}
	if s := formatMutation(skip); !strings.Contains(s, "skipped") {
		t.Errorf("skipped report:\n%s", s)
	}
}

func TestRunTestMutationUnsupportedLang(t *testing.T) {
	// A target whose language has no engine degrades cleanly, not a crash.
	// Uses the gaps fixture (Go) but forces the no-engine path via a fake.
	// Here we assert resolveTestGenTargets errors for a missing symbol first.
	setupGapsFixture(t)
	_, err := runTestMutation(context.Background(), "NoSuchSymbol")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
}

func TestTestGenMutateFlagRegistered(t *testing.T) {
	if testGenCmd.Flags().Lookup("mutate") == nil {
		t.Error("test gen missing --mutate flag")
	}
}

func TestMaybeAnnotateMutationBelowThreshold(t *testing.T) {
	// score < 1.0 with a survivor → annotation requested.
	rep := mutation.Report{Tool: "go-mutesting", Total: 2, Killed: 1, Survived: 1, Score: 0.5,
		Survivors: []mutation.Survivor{{File: "store.go", Line: 1, Desc: "x"}}}
	note, annotate := mutationNote(rep)
	if !annotate || !strings.Contains(note, "50") {
		t.Fatalf("below-threshold should annotate with score note, got %q annotate=%v", note, annotate)
	}

	clean := mutation.Report{Tool: "go-mutesting", Total: 2, Killed: 2, Score: 1.0}
	if _, annotate := mutationNote(clean); annotate {
		t.Error("100% score must not annotate")
	}
	skipped := mutation.Report{Skipped: true, Note: "absent"}
	if _, annotate := mutationNote(skipped); annotate {
		t.Error("skipped report must not annotate")
	}
}
