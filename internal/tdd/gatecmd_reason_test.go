package tdd

// RED-phase tests for feature gate-reason (spec.md C1, design.md D5): GateResult
// gains a machine-readable Reason field, set at every RunGate return site, plus
// DeterministicJudge distinguishing coverage_missing from suite_failing. The
// Reason field does not exist yet, so every assertion on it goes through
// json.Marshal of the GateResult and a substring check on the marshaled bytes —
// that compiles against today's GateResult (no "reason" key emitted) and fails
// at runtime, which is the RED this stage requires. Feedback-string assertions
// are plain field reads: they already compile and pass today, and pin the
// byte-unchanged human text this feature must not touch.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// logSafe defangs go test's own "[build failed]"/"[setup failed]" banners
// inside a string before it is echoed into a t.Fatalf/t.Errorf message. This
// package's RED gate classifies build_failed by grepping raw test output for
// those literal banners; without this, a fixture whose real (asserted)
// feedback legitimately contains one — e.g. TestGateReasonBuildFailed's go
// test build-failure output — would have the SAME literal reappear in this
// file's own failure log, and the gate would misread a runtime-red assertion
// failure here as a build failure. The assertions themselves always compare
// the real, unsanitized value; only what gets printed on failure changes.
func logSafe(s string) string {
	s = strings.ReplaceAll(s, "[build failed]", "[build-failed]")
	s = strings.ReplaceAll(s, "[setup failed]", "[setup-failed]")
	return s
}

// logSafeGate renders res for a failure message with logSafe applied.
func logSafeGate(res GateResult) string {
	return logSafe(fmt.Sprintf("%+v", res))
}

// marshaledGate returns the JSON tdd gate would print for res, for substring
// assertions on fields (like Reason) that do not exist on GateResult yet.
func marshaledGate(t *testing.T, res GateResult) string {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal gate result: %v", err)
	}
	return string(b)
}

// @s1: green gate pass reports reason ok.
func TestGateReasonOkOnGreenPass(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root) // count.feature with tags @s1,@s2

	base := tddDir(root)
	if err := writeRedBaseline(base, "count", root, nil); err != nil {
		t.Fatalf("writeRedBaseline: %v", err)
	}

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want ok, got %s", logSafeGate(res))
	}

	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"ok":true`) {
		t.Fatalf("marshaled result missing ok:true, got %s", logSafe(marshaled))
	}
	if !strings.Contains(marshaled, `"reason":"ok"`) {
		t.Fatalf("marshaled result missing reason:ok, got %s", logSafe(marshaled))
	}
}

// @s2: red gate on a green suite reports reason not_red.
func TestGateReasonNotRedOnGreenSuite(t *testing.T) {
	root := t.TempDir() // no go.mod: the Go per-file scoped RED proof never fires

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}} // fake runner that passes
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "", "red", "spec/count_test.js", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on a green suite driving a red gate, got %s", logSafeGate(res))
	}
	if res.Feedback != "suite is green — no failing test drove the change" {
		t.Fatalf("feedback = %q, want the unchanged suite-green message", logSafe(res.Feedback))
	}

	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"not_red"`) {
		t.Fatalf("marshaled result missing reason:not_red, got %s", logSafe(marshaled))
	}
}

// @s3: red gate on a Go test file that does not compile reports build_failed.
func TestGateReasonBuildFailed(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	// The new test file references an undefined symbol: a build failure, not a
	// legitimately-failing test.
	writeFileT(t, root, "pkg/count_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n\tundefinedFunc()\n}\n")

	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}} // fake runner that fails
	ctx := context.Background()

	res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "pkg/count_test.go", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok for a new test file that fails to build, got %s", logSafeGate(res))
	}
	if !strings.Contains(res.Feedback, "new test file pkg/count_test.go fails to build") {
		t.Fatalf("feedback = %q, want it to start with the unchanged build-failed prefix", logSafe(res.Feedback))
	}

	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"build_failed"`) {
		t.Fatalf("marshaled result missing reason:build_failed, got %s", logSafe(marshaled))
	}
}

// @s4: green gate with a mutated red baseline reports baseline_mutated.
func TestGateReasonBaselineMutated(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root) // count.feature with tags @s1,@s2
	writeFileT(t, root, "pkg/count_test.go", "package pkg\n")

	base := tddDir(root)
	if err := writeRedBaseline(base, "count", root, []string{"pkg/count_test.go"}); err != nil {
		t.Fatalf("writeRedBaseline: %v", err)
	}
	// Mutate the baselined test file after RED.
	writeFileT(t, root, "pkg/count_test.go", "package pkg\n// weakened\n")

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on a mutated baselined test file, got %s", logSafeGate(res))
	}
	if res.Feedback != "test files mutated since RED: pkg/count_test.go" {
		t.Fatalf("feedback = %q, want the unchanged mutated-file message", logSafe(res.Feedback))
	}

	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"baseline_mutated"`) {
		t.Fatalf("marshaled result missing reason:baseline_mutated, got %s", logSafe(marshaled))
	}
}

// @s5: green gate with a missing red baseline reports baseline_mutated.
func TestGateReasonBaselineMissing(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root) // count.feature; no red-baseline.json written in its base dir

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok when no red baseline exists, got %s", logSafeGate(res))
	}
	if res.Feedback != "no red baseline found for this feature — run `tdd gate --expect red` first" {
		t.Fatalf("feedback = %q, want the unchanged missing-baseline message", logSafe(res.Feedback))
	}

	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"baseline_mutated"`) {
		t.Fatalf("marshaled result missing reason:baseline_mutated, got %s", logSafe(marshaled))
	}
}

// @s6: the judge distinguishes coverage_missing (an uncovered scenario) from
// suite_failing (full coverage but a red test run).
func TestGateReasonJudgeCoverageVsSuiteFailing(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root) // count.feature with tags @s1,@s2

	base := tddDir(root)
	if err := writeRedBaseline(base, "count", root, nil); err != nil {
		t.Fatalf("writeRedBaseline: %v", err)
	}
	ctx := context.Background()

	// @s2 is not covered: the deterministic judge must reject on coverage
	// before ever invoking the test runner.
	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate (coverage_missing): %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on partial coverage, got %s", logSafeGate(res))
	}
	marshaled := marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"coverage_missing"`) {
		t.Fatalf("marshaled result missing reason:coverage_missing, got %s", logSafe(marshaled))
	}

	// Full coverage but the suite fails: the judge must report suite_failing,
	// not coverage_missing.
	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	res, err = RunGate(ctx, redCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate (suite_failing): %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on a failing suite with full coverage, got %s", logSafeGate(res))
	}
	marshaled = marshaledGate(t, res)
	if !strings.Contains(marshaled, `"reason":"suite_failing"`) {
		t.Fatalf("marshaled result missing reason:suite_failing, got %s", logSafe(marshaled))
	}
}
