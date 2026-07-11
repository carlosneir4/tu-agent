package tdd

import (
	"context"
	"fmt"
	"strings"
)

// reviewFixerBudget bounds how many review→fix ROUNDS the post-loop review
// spends on critical/important findings before it accepts the run as
// "pass with pending findings". Each round dispatches the fixer, verifies the
// suite, and — only if the suite is green — re-reviews.
const reviewFixerBudget = 2

// reviewRefeedBudget bounds how many times a SINGLE round re-feeds the fixer
// after its fix left the suite red. It is independent of reviewFixerBudget:
// this bounds the inner red-refeed loop (so a fixer that can never restore
// green cannot spin forever), while reviewFixerBudget bounds the outer rounds.
// A round that exhausts this budget gives up with the suite still red and
// reports that back to the caller instead of pretending it went green.
const reviewRefeedBudget = 2

// runPostLoopReview runs the whole-branch review exactly once after every
// feature passed. It records progress on state.Review ("pending" on entry,
// then "pass" or "skipped"), persisting each transition. A skip reason from the
// review scope (unresolvable merge-base or empty diff) warns visibly and never
// fails the run; a scope error is likewise non-fatal (warns and leaves Review
// "pending" so a rerun resumes into the review). The review itself never blocks
// a run whose features already passed their gates.
func runPostLoopReview(ctx context.Context, o Options, runner StageRunner, st *State, statePath string) error {
	// Idempotent: a review already resolved on a prior (resumed) run is not re-run.
	if st.Review == "pass" || st.Review == "skipped" {
		return nil
	}
	// Persist "pending" BEFORE computing the scope: a transient scope failure
	// then resumes straight back into the review instead of forfeiting the whole
	// completed feature loop.
	st.Review = "pending"
	if err := SaveState(statePath, *st); err != nil {
		return fmt.Errorf("tdd.runPostLoopReview: %w", err)
	}
	base, files, skipReason, err := o.ReviewScope(ctx, o.WorkDir)
	if err != nil {
		// Non-fatal: a scope failure is a transient environment problem, not a
		// review outcome. Warn visibly and leave Review "pending" so the next run
		// resumes into the review rather than restarting the completed loop.
		fmt.Fprintf(o.Out, "review scope failed: %v — leaving review pending; rerun to retry.\n", err)
		return nil
	}
	if skipReason != "" {
		fmt.Fprintf(o.Out, "review skipped: %s\n", skipReason)
		st.Review = "skipped"
		if err := SaveState(statePath, *st); err != nil {
			return fmt.Errorf("tdd.runPostLoopReview: %w", err)
		}
		return nil
	}
	resolved, err := runReviewRounds(ctx, o, runner, base, files)
	if err != nil {
		return err
	}
	if !resolved {
		// The review did not resolve (e.g. a malformed reviewer emitted no
		// verdict). Non-fatal, consistent with the review path: leave Review
		// "pending" (already persisted) so the next run resumes into it.
		return nil
	}
	st.Review = "pass"
	if err := SaveState(statePath, *st); err != nil {
		return fmt.Errorf("tdd.runPostLoopReview: %w", err)
	}
	return nil
}

// runReviewRounds dispatches the review stage, then routes critical/important
// findings to the review-fixer within a fixed budget of rounds. Minor-only or
// empty findings pass immediately; persistent criticals after the budget end
// the run pass-with-pending-findings (an explicit message, never blocked). If a
// fixer round gives up with the suite still red, the round stops WITHOUT
// re-reviewing (a re-review would proceed as if the suite were green) and the
// red state is made visible.
// It returns resolved=true when the review reached a definite outcome (blocking
// or not) that should persist as "pass", and resolved=false when the review did
// not resolve (a malformed reviewer emitted no verdict) and must be left
// "pending" for a rerun — never a silent pass.
func runReviewRounds(ctx context.Context, o Options, runner StageRunner, base string, files []string) (bool, error) {
	reviewTask := reviewTaskText(base, files)
	verdict, err := dispatchReview(ctx, runner, reviewTask)
	if err != nil {
		return false, err
	}
	if verdict == nil {
		return false, warnNoVerdict(o)
	}
	for round := 0; hasBlockingFindings(verdict) && round < reviewFixerBudget; round++ {
		green, giveUp, err := runFixerRound(ctx, o, runner, verdict, base)
		if err != nil {
			return false, err
		}
		if !green {
			// The fixer round gave up (suite left red, or forbidden test edits it
			// never reverted). Do NOT re-review — the diff is not in a trustworthy
			// green state. Surface the round's own accurate reason.
			fmt.Fprintln(o.Out, giveUp)
			return true, nil
		}
		verdict, err = dispatchReview(ctx, runner, reviewTask)
		if err != nil {
			return false, err
		}
		if verdict == nil {
			return false, warnNoVerdict(o)
		}
	}
	if hasBlockingFindings(verdict) {
		fmt.Fprintln(o.Out, "review complete: run passed with pending review findings — features already passed their gates.")
	} else {
		fmt.Fprintln(o.Out, "review complete: no blocking findings.")
	}
	return true, nil
}

// warnNoVerdict handles a review contract that carried no verdict. The per-
// feature judge path hard-fails the identical malformation (flow.go), but the
// review path is never fatal: a malformed reviewer output is indistinguishable
// from a clean branch, so it must not silently pass. Warn visibly and leave the
// review unresolved (Review stays "pending") so a rerun retries it.
func warnNoVerdict(o Options) error {
	fmt.Fprintln(o.Out, "review returned no verdict — the reviewer output is malformed; treating the review as incomplete and leaving it pending. Rerun to retry.")
	return nil
}

// runFixerRound dispatches the review-fixer for the current findings, then runs
// the injected test suite. A red suite is re-fed to the fixer within the SAME
// round (the failing output rides the next task) until it is green again. The
// re-feed loop is bounded by reviewRefeedBudget so a fixer that can never
// restore green cannot spin forever. It returns green=true (giveUp="") when the
// suite is green (the caller may re-review); on green=false the round gave up
// and giveUp carries the accurate reason to surface (suite left red, or
// forbidden test edits the fixer never reverted) — the caller must NOT
// re-review.
func runFixerRound(ctx context.Context, o Options, runner StageRunner, verdict *Verdict, base string) (bool, string, error) {
	// Deterministic test-immutability guard, mirroring the sandwich's
	// PartitionTests-on-diff check (sandwich.go). Active only when Snapshot/Diff
	// are injected (the same gate as sandwichEnabled); without them the rule
	// stays prompt-only. A fixer that weakens or deletes a test could make the
	// suite pass falsely, so a touched test file is never a success.
	guard := o.Snapshot != nil && o.Diff != nil
	// Snapshot the pre-fixer-round baseline ONCE, mirroring the sandwich's
	// once-per-round baseline (sandwich.go:162). Diffing every attempt against
	// this fixed tree — rather than re-snapshotting per attempt — means forbidden
	// test edits from a rejected attempt cannot escape detection on a later clean
	// attempt, and a compliant revert is not re-flagged as a fresh violation.
	var before string
	if guard {
		var err error
		if before, err = o.Snapshot(ctx); err != nil {
			return false, "", fmt.Errorf("tdd.runFixerRound: %w", err)
		}
	}
	task := fixerTaskText(verdict.Findings, base)
	for refeed := 0; ; refeed++ {
		if _, err := runner.Run(ctx, "review-fixer", task); err != nil {
			return false, "", err
		}
		if guard {
			touched, err := touchedTestFiles(ctx, o, before)
			if err != nil {
				return false, "", err
			}
			if len(touched) > 0 {
				// Forbidden: a modified test file. Treat as a failed attempt and
				// re-feed within the same round (consuming the refeed budget);
				// never re-verify or accept it as green.
				if refeed >= reviewRefeedBudget {
					// The suite was never run on this path, so it was NOT "left red":
					// the round gave up because forbidden test edits remain unreverted.
					return false, "review incomplete: the review-fixer repeatedly modified test files (forbidden) and never reverted them — these test edits remain in the working tree and must be reverted before merging: " +
						strings.Join(touched, ", ") + "; the run still passed its feature gates.", nil
				}
				task = fixerTouchedTestsTask(verdict.Findings, touched, base)
				continue
			}
		}
		passed, output, err := o.Runner(ctx)
		if err != nil {
			return false, "", fmt.Errorf("tdd.runFixerRound: %w", err)
		}
		if passed {
			return true, "", nil
		}
		if refeed >= reviewRefeedBudget {
			return false, "review incomplete: the test SUITE was left RED by the review-fixer — resolve it before merging; the run still passed its feature gates.", nil
		}
		task = fixerRefeedTask(verdict.Findings, output, base)
	}
}

// touchedTestFiles snapshots the tree after a fixer dispatch and returns the
// test files it changed since before, using the same PartitionTests split the
// sandwich uses to reject an implementer that mutates tests.
func touchedTestFiles(ctx context.Context, o Options, before string) ([]string, error) {
	after, err := o.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("tdd.touchedTestFiles: %w", err)
	}
	changed, err := o.Diff(ctx, before, after)
	if err != nil {
		return nil, fmt.Errorf("tdd.touchedTestFiles: %w", err)
	}
	tests, _ := PartitionTests(changed)
	return tests, nil
}

// dispatchReview runs the review stage and returns its verdict (nil when the
// stage emitted a contract without one — the caller treats that as an
// incomplete review, never a silent pass).
func dispatchReview(ctx context.Context, runner StageRunner, task string) (*Verdict, error) {
	c, err := runner.Run(ctx, "review", task)
	if err != nil {
		return nil, err
	}
	return c.Verdict, nil
}

// hasBlockingFindings reports whether a verdict carries any critical or
// important finding — the only severities the fixer acts on (minor is
// report-only).
func hasBlockingFindings(v *Verdict) bool {
	if v == nil {
		return false
	}
	for _, f := range v.Findings {
		if f.Severity == "critical" || f.Severity == "important" {
			return true
		}
	}
	return false
}

// reviewTaskText builds the whole-branch review task: the merge-base and the
// changed files the reviewer must scope to.
func reviewTaskText(base string, files []string) string {
	return "Review the whole branch diff since merge-base " + base +
		".\nChanged files:\n" + strings.Join(files, "\n")
}

// fixerTaskText builds the review-fixer task carrying the findings to resolve.
func fixerTaskText(findings []Finding, base string) string {
	return "Resolve these whole-branch review findings without touching any test " +
		"file; the entire suite must stay green.\nBranch base " + base + ".\n" +
		formatFindings(findings)
}

// fixerRefeedTask re-feeds the fixer within the same round after its fix left
// the suite red, appending the failing-suite output.
func fixerRefeedTask(findings []Finding, suiteOutput, base string) string {
	return fixerTaskText(findings, base) +
		"\nYour previous fix left the test suite RED. Failing output:\n" + suiteOutput
}

// fixerTouchedTestsTask re-feeds the fixer within the same round after it
// modified test files (forbidden), naming the offending tests so it reverts
// them and fixes production only.
func fixerTouchedTestsTask(findings []Finding, tests []string, base string) string {
	return fixerTaskText(findings, base) +
		"\nYour previous fix MODIFIED test files, which is forbidden — revert those changes and fix production code only: " +
		strings.Join(tests, ", ")
}

// formatFindings renders findings as one bullet per finding.
func formatFindings(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", f.Severity, f.Location, f.Summary)
	}
	return b.String()
}
