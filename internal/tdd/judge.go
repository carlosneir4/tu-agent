package tdd

import (
	"context"
	"fmt"
	"strings"
)

// TestRunner runs the project test command and reports pass/fail plus output.
type TestRunner func(ctx context.Context) (passed bool, output string, err error)

// DetResult is the outcome of the deterministic judge pre-checks. Reason is the
// same enum-by-convention as GateResult.Reason: ok | not_red | coverage_missing |
// suite_failing | runner_error.
type DetResult struct {
	OK       bool
	Feedback string
	Reason   string
}

// normTag canonicalizes a scenario tag for comparison: trimmed, without a
// leading '@'. The plugin conductor builds the gate's --covered list by hand and
// often drops (or adds) the '@', so coverage must match regardless of the prefix.
func normTag(t string) string {
	return strings.TrimPrefix(strings.TrimSpace(t), "@")
}

// CheckCoverage verifies every featureTag is present in covered. Tags are matched
// after normalizing the leading '@' on both sides; the original featureTag form is
// preserved in the "missing" feedback.
func CheckCoverage(featureTags, covered []string) DetResult {
	set := make(map[string]bool, len(covered))
	for _, t := range covered {
		set[normTag(t)] = true
	}
	missing := make([]string, 0)
	for _, t := range featureTags {
		if !set[normTag(t)] {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return DetResult{Feedback: "scenarios without a test: " + strings.Join(missing, ", ")}
	}
	return DetResult{OK: true}
}

// DeterministicJudge gates on @s coverage, then a green test run. It runs in Go
// so a structurally-broken result is rejected without spending a model call.
func DeterministicJudge(ctx context.Context, run TestRunner, featureTags, covered []string) DetResult {
	if cov := CheckCoverage(featureTags, covered); !cov.OK {
		cov.Reason = "coverage_missing"
		return cov
	}
	passed, output, err := run(ctx)
	if err != nil {
		return DetResult{Feedback: fmt.Sprintf("test runner error: %v", err), Reason: "runner_error"}
	}
	if !passed {
		return DetResult{Feedback: "tests are red:\n" + tail(output, 20), Reason: "suite_failing"}
	}
	return DetResult{OK: true, Reason: "ok"}
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
