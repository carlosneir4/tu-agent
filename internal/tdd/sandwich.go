package tdd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tu/tu-agent/internal/testresult"
)

// SandwichDeps injects the effectful collaborators so RunSandwich is testable
// without git or a real build.
type SandwichDeps struct {
	Runner      StageRunner
	Tests       TestRunner
	Snapshot    func(ctx context.Context) (string, error)
	Diff        func(ctx context.Context, from, to string) ([]string, error)
	LoadReports func(since time.Time) (testresult.Report, error)
	Out         io.Writer
	// Strict runs the sandwich one @s at a time (test->red->impl->green->next)
	// instead of batching all of a sub-feature's tests through one RED->GREEN
	// cycle. Default false (batch).
	Strict bool
	// Budget bounds the per-phase retry attempts (RED and GREEN each). <=0 means 1.
	Budget int
}

// SandwichResult is the outcome of one batch RED->GREEN cycle for a feature.
type SandwichResult struct {
	OK             bool
	Feedback       string
	Regressions    []string // @s tags reclassified as green-on-arrival regression
	SourceArtifact Artifact // the implementer's primary source file, for the mutation gate (empty for regression/refactor)
}

// firstSourceArtifact returns the first kind:"source" artifact in c, or a zero Artifact.
func firstSourceArtifact(c Contract) Artifact {
	for _, a := range c.Artifacts {
		if a.Kind == "source" {
			return a
		}
	}
	return Artifact{}
}

// RunSandwich runs the sandwich for one feature: dispatch the test-writer,
// verify the new tests are RED (guarding that no production was written), then
// dispatch the implementer, verify GREEN (guarding that no test was modified).
// Green-on-arrival test files are reclassified as regression, not failed.
//
// By default (d.Strict false) it runs one batch RED->GREEN cycle across all of
// featureTags. When d.Strict is true, it instead loops one @s scenario at a
// time — write one test, verify it red, implement, verify green — before
// moving to the next scenario, reusing the same cycle and guards per scenario.
func RunSandwich(ctx context.Context, d SandwichDeps, feature string, featureTags, covered []string) SandwichResult {
	if d.Strict {
		return runSandwichStrict(ctx, d, feature, featureTags)
	}
	return runSandwichCycle(ctx, d, feature, featureTags, covered)
}

// runSandwichStrict runs runSandwichCycle once per scenario tag, in order,
// scoping the test-writer/implementer dispatch and the deterministic gate to
// that single scenario each time. It stops at the first failing scenario.
func runSandwichStrict(ctx context.Context, d SandwichDeps, feature string, featureTags []string) SandwichResult {
	var regressions []string
	var source Artifact
	for _, tag := range featureTags {
		res := runSandwichCycle(ctx, d, feature, []string{tag}, []string{tag})
		regressions = append(regressions, res.Regressions...)
		if res.SourceArtifact.Path != "" {
			source = res.SourceArtifact
		}
		if !res.OK {
			return SandwichResult{Feedback: res.Feedback, Regressions: regressions, SourceArtifact: source}
		}
	}
	return SandwichResult{OK: true, Regressions: regressions, SourceArtifact: source}
}

// runSandwichCycle runs one batch RED->GREEN cycle for the given scenario
// tags: dispatch the test-writer, verify RED, dispatch the implementer, verify
// GREEN. Both the batch path and the strict per-scenario loop share this.
//
// Each phase retries in place (up to d.Budget attempts) with feedback from the
// previous attempt, so a rejected RED or GREEN attempt re-enters at the phase
// that actually failed instead of forcing the caller to restart the whole
// cycle (which would re-run the test-writer against tests that already exist).
func runSandwichCycle(ctx context.Context, d SandwichDeps, feature string, featureTags, covered []string) SandwichResult {
	budget := d.Budget
	if budget <= 0 {
		budget = 1
	}

	// ---- RED phase ----
	before, err := d.Snapshot(ctx)
	if err != nil {
		return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", err)}
	}
	var newTests []string
	redFeedback := ""
	redDone := false
	for attempt := 0; attempt < budget; attempt++ {
		task := "Write the failing tests for feature " + feature + " (scenarios: " + strings.Join(featureTags, ", ") + ")."
		if redFeedback != "" {
			task += "\n\nYour previous attempt was rejected:\n" + redFeedback
		}
		if _, derr := d.Runner.Run(ctx, "craftsman", TestWriterPrompt+"\n\n"+task); derr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("test-writer dispatch: %v", derr)}
		}
		afterTests, serr := d.Snapshot(ctx)
		if serr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", serr)}
		}
		changed, cerr := d.Diff(ctx, before, afterTests)
		if cerr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("diff: %v", cerr)}
		}
		var prod []string
		newTests, prod = PartitionTests(changed)
		if len(prod) > 0 {
			// leftover artifacts persist between attempts, so tell the agent to remove them.
			redFeedback = "you created production files — remove them and write ONLY tests: " + strings.Join(prod, ", ")
			continue
		}
		if len(newTests) == 0 {
			redFeedback = "you wrote no test files — write the failing tests first"
			continue
		}
		since := time.Now()
		passed, _, rerr := d.Tests(ctx)
		if rerr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("test runner error: %v", rerr)}
		}
		rep, lerr := d.LoadReports(since)
		if lerr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("load reports: %v", lerr)}
		}
		red := NewTestsRed(passed, rep, newTests)
		if red.OK {
			redDone = true
			break
		}
		if len(red.GreenFiles) == len(newTests) && len(red.GreenFiles) > 0 {
			// Every new test is green-on-arrival: pure regression coverage, no prod
			// was written (guarded above). Accept as regression, skip GREEN phase.
			// The whole batch was called with featureTags, so every scenario tag
			// in it is regression coverage — ScenarioState is keyed by @s tag, not
			// file path.
			fmt.Fprintf(d.Out, "sandwich: %s — reclassified as regression\n", red.Feedback)
			return SandwichResult{OK: true, Regressions: featureTags}
		}
		redFeedback = red.Feedback
	}
	if !redDone {
		return SandwichResult{Feedback: "RED phase failed after retries: " + redFeedback}
	}

	// ---- GREEN phase ----
	afterTests, err := d.Snapshot(ctx)
	if err != nil {
		return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", err)}
	}
	greenFeedback := ""
	for attempt := 0; attempt < budget; attempt++ {
		task := "Implement minimal production for feature " + feature + " to make its tests pass."
		if greenFeedback != "" {
			task += "\n\nYour previous attempt was rejected:\n" + greenFeedback
		}
		implContract, derr := d.Runner.Run(ctx, "craftsman", ImplementerPrompt+"\n\n"+task)
		if derr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("implementer dispatch: %v", derr)}
		}
		afterImpl, serr := d.Snapshot(ctx)
		if serr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", serr)}
		}
		changedImpl, cerr := d.Diff(ctx, afterTests, afterImpl)
		if cerr != nil {
			return SandwichResult{Feedback: fmt.Sprintf("diff: %v", cerr)}
		}
		if touchedTests, _ := PartitionTests(changedImpl); len(touchedTests) > 0 {
			greenFeedback = "you modified test files (cannot weaken tests): " + strings.Join(touchedTests, ", ")
			continue
		}
		if det := DeterministicJudge(ctx, d.Tests, featureTags, covered); !det.OK {
			greenFeedback = det.Feedback
			continue
		}
		return SandwichResult{OK: true, SourceArtifact: firstSourceArtifact(implContract)}
	}
	return SandwichResult{Feedback: "GREEN phase failed after retries: " + greenFeedback}
}

// RunRefactor handles a refactor sub-feature: NO RED phase. It dispatches a
// refactor pass (which may touch tests) and requires the whole existing suite to
// stay green, retrying with feedback up to d.Budget. The result is never credited
// as TDD (Regressions stays empty; the conductor marks scenarios kind=refactor).
func RunRefactor(ctx context.Context, d SandwichDeps, feature string, featureTags []string, feedback string) SandwichResult {
	budget := d.Budget
	if budget <= 0 {
		budget = 1
	}
	for attempt := 0; attempt < budget; attempt++ {
		task := "Refactor for feature " + feature + ": improve structure WITHOUT changing behavior; keep the ENTIRE test suite green."
		if feedback != "" {
			task += "\n\nAddress this feedback:\n" + feedback
		}
		if _, err := d.Runner.Run(ctx, "craftsman", RefactorPrompt+"\n\n"+task); err != nil {
			return SandwichResult{Feedback: fmt.Sprintf("refactor dispatch: %v", err)}
		}
		if det := DeterministicJudge(ctx, d.Tests, featureTags, featureTags); det.OK {
			return SandwichResult{OK: true}
		} else {
			feedback = det.Feedback
		}
	}
	return SandwichResult{Feedback: "refactor left the suite red after retries: " + feedback}
}

// RefineSandwich re-dispatches the implementer to address judge feedback on
// already-green code, WITHOUT a RED phase: the tests already exist and pass.
// It guards that no test file is modified and that the suite stays green.
// The conductor calls this on a judge "revise" instead of re-running RunSandwich.
func RefineSandwich(ctx context.Context, d SandwichDeps, feature string, featureTags []string, feedback string) SandwichResult {
	before, err := d.Snapshot(ctx)
	if err != nil {
		return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", err)}
	}
	task := "Refine the production for feature " + feature +
		" to address this review feedback. Do NOT modify, add, or delete any test; keep every test green:\n\n" + feedback
	contract, derr := d.Runner.Run(ctx, "craftsman", ImplementerPrompt+"\n\n"+task)
	if derr != nil {
		return SandwichResult{Feedback: fmt.Sprintf("refine dispatch: %v", derr)}
	}
	after, err := d.Snapshot(ctx)
	if err != nil {
		return SandwichResult{Feedback: fmt.Sprintf("snapshot: %v", err)}
	}
	changed, err := d.Diff(ctx, before, after)
	if err != nil {
		return SandwichResult{Feedback: fmt.Sprintf("diff: %v", err)}
	}
	if touchedTests, _ := PartitionTests(changed); len(touchedTests) > 0 {
		return SandwichResult{Feedback: "refine modified test files (cannot weaken tests): " + strings.Join(touchedTests, ", ")}
	}
	if det := DeterministicJudge(ctx, d.Tests, featureTags, featureTags); !det.OK {
		return SandwichResult{Feedback: det.Feedback}
	}
	return SandwichResult{OK: true, SourceArtifact: firstSourceArtifact(contract)}
}
