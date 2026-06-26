package tdd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Options configures one flow run.
type Options struct {
	Analyst           Chatter
	Dispatcher        Dispatcher
	Runner            TestRunner
	In                io.Reader
	Out               io.Writer
	Task              string
	Branch            string
	WorkDir           string
	FeatureReader     func(name string) (string, error)
	Budget            int
	Mutator           Mutator
	MutationThreshold float64
	Archive           bool
}

// Result is the terminal outcome of a flow run.
type Result struct {
	Status   string
	Features []FeatureState
}

// Run drives the durable, multi-feature pipeline. A resumable state on disk lets
// an interrupted run continue per feature; otherwise it interrogates, designs,
// signs the feature list, and runs each feature through the TDD loop.
func Run(ctx context.Context, o Options) (Result, error) {
	if o.Budget <= 0 {
		o.Budget = 3
	}
	reader := bufio.NewReader(o.In)
	runner := StageRunner{D: o.Dispatcher}
	statePath := filepath.Join(o.WorkDir, "state.json")

	st, _ := LoadState(statePath)
	resume := st.Resumable() && resumeApproves(reader, o.Out, st)

	if !resume {
		if _, err := RunAnalyst(ctx, o.Analyst, o.Task, reader, o.Out); err != nil {
			return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
		}
		var feats []FeaturePlan
		archFeedback := ""
		approved := false
		// Design loop: at most 3 architect→human-gate rounds (revise feedback is
		// fed back to the architect); not user-configurable yet.
		for designBudget := 3; designBudget > 0; designBudget-- {
			task := "Read .tu-agent/tdd/spec.md and produce the design, scenarios, and complexity classification."
			if archFeedback != "" {
				task += "\n\nThe user rejected the previous design:\n" + archFeedback + "\nRevise the design accordingly."
			}
			arch, err := runner.Run(ctx, "architect", task)
			if err != nil {
				return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
			}
			if arch.Complexity == ComplexityTrivial {
				feature := arch.Handoff
				if _, err := runner.Run(ctx, "craftsman",
					"Implement the trivial change directly; keep existing tests green. handoff: "+feature); err != nil {
					return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
				}
				passed, _, rerr := o.Runner(ctx)
				if rerr != nil || !passed {
					return Result{Status: StatusBlocked}, nil
				}
				fmt.Fprintln(o.Out, "trivial path complete — tests green.")
				return Result{Status: StatusPass}, nil
			}
			feats = planFeatures(arch)
			if len(feats) == 0 {
				fmt.Fprintln(o.Out, "architect produced no features — stopping.")
				return Result{Status: StatusBlocked}, nil
			}
			decision, fb := humanGate(reader, o.Out, feats)
			if decision == "approved" {
				approved = true
				break
			}
			if decision == "stop" {
				fmt.Fprintln(o.Out, "human gate: aborted — stopping.")
				return Result{Status: StatusBlocked}, nil
			}
			archFeedback = fb
			fmt.Fprintf(o.Out, "human gate: revising design (rounds left %d)\n", designBudget-1)
		}
		if !approved {
			fmt.Fprintln(o.Out, "design budget exhausted — stopping.")
			return Result{Status: StatusBlocked}, nil
		}
		names := make([]string, 0, len(feats))
		for _, f := range feats {
			names = append(names, f.Name)
		}
		st = BeginRun(o.Task, o.Branch, names)
		if err := SaveState(statePath, st); err != nil {
			return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
		}
	}

	for {
		name, ok := st.NextPending()
		if !ok {
			break
		}
		featSrc, err := o.FeatureReader(name)
		if err != nil {
			return Result{Status: StatusBlocked, Features: st.Features}, fmt.Errorf("tdd.Run: read feature %s: %w", name, err)
		}
		status, ferr := runFeatureTDD(ctx, o, runner, name, ScenarioTags(featSrc))
		if ferr != nil {
			return Result{Status: StatusBlocked, Features: st.Features}, fmt.Errorf("tdd.Run: %w", ferr)
		}
		st.Mark(name, status)
		if err := SaveState(statePath, st); err != nil {
			return Result{Status: StatusBlocked, Features: st.Features}, fmt.Errorf("tdd.Run: %w", err)
		}
		if status == StatusBlocked {
			fmt.Fprintf(o.Out, "feature %s blocked — stopping run (remaining stay pending).\n", name)
			break
		}
	}

	pass, pending, blocked := st.Summary()
	fmt.Fprintf(o.Out, "run summary: %d pass, %d blocked, %d pending\n", pass, blocked, pending)
	overall := StatusPass
	if blocked > 0 || pending > 0 {
		overall = StatusBlocked
	}
	return Result{Status: overall, Features: st.Features}, nil
}

// runFeatureTDD runs the standard TDD loop for one feature, returning its
// terminal status. A nil error with StatusBlocked means a logical failure
// (budget/fail); a non-nil error means a stage dispatch could not run.
func runFeatureTDD(ctx context.Context, o Options, runner StageRunner, feature string, featureTags []string) (string, error) {
	feedback := ""
	for budget := o.Budget; budget > 0; budget-- {
		task := "Implement the approved feature by strict TDD. handoff: " + feature
		if feedback != "" {
			task += "\n\nJudge feedback to address:\n" + feedback
		}
		craft, err := runner.Run(ctx, "craftsman", task)
		if err != nil {
			return StatusBlocked, err
		}
		if det := DeterministicJudge(ctx, o.Runner, featureTags, craft.Scenarios); !det.OK {
			feedback = det.Feedback
			fmt.Fprintf(o.Out, "deterministic gate: %s (budget %d)\n", det.Feedback, budget-1)
			continue
		}
		judge, err := runner.Run(ctx, "judge",
			"Judge design and discipline for feature "+feature+". The deterministic gate already passed.")
		if err != nil {
			return StatusBlocked, err
		}
		if judge.Verdict == nil {
			return StatusBlocked, fmt.Errorf("judge returned no verdict for %s", feature)
		}
		switch judge.Verdict.Result {
		case "pass":
			if o.Mutator != nil {
				if mt, ok := MutationTargetFromContract(craft); ok {
					out := o.Mutator(ctx, mt)
					if out.Skipped {
						fmt.Fprintf(o.Out, "mutation gate: skipped — %s\n", out.Note)
					} else if mg := MutationGate(o.MutationThreshold, out); !mg.OK {
						feedback = mg.Feedback
						fmt.Fprintf(o.Out, "mutation gate: %s (budget %d)\n", mg.Feedback, budget-1)
						continue
					}
				}
			}
			if o.Archive {
				if _, serr := runner.Run(ctx, "scribe",
					"Archive the completed feature "+feature+": read the spec and progress, then mem_save a decision/<feature> note with what changed and why."); serr != nil {
					fmt.Fprintf(o.Out, "scribe: archive skipped — %v\n", serr)
				}
			}
			fmt.Fprintf(o.Out, "feature %s done — judge approved.\n", feature)
			return StatusPass, nil
		case "revise":
			feedback = judge.Verdict.Feedback
			fmt.Fprintf(o.Out, "judge: revise — %s (budget %d)\n", feedback, budget-1)
		default: // "fail"
			return StatusBlocked, nil
		}
	}
	fmt.Fprintf(o.Out, "retry budget exhausted — feature %s blocked.\n", feature)
	return StatusBlocked, nil
}

// humanGate shows the feature list and reads one decision line: "approved" to
// start TDD, "stop" (or empty) to abort, or any other text taken as revise
// feedback for the architect.
func humanGate(reader *bufio.Reader, out io.Writer, feats []FeaturePlan) (decision, feedback string) {
	fmt.Fprintf(out, "\nProposed %d feature(s):\n", len(feats))
	for _, f := range feats {
		fmt.Fprintf(out, "  - %s (%d scenarios)\n", f.Name, len(f.Scenarios))
	}
	fmt.Fprint(out, "Type 'approved' to start TDD, 'stop' to abort, or describe what to change: ")
	line, _ := reader.ReadString('\n')
	s := strings.TrimSpace(line)
	switch s {
	case "approved":
		return "approved", ""
	case "stop", "":
		return "stop", ""
	default:
		return "revise", s
	}
}

// resumeApproves shows the resumable summary and reads one line; "resume"
// continues, anything else falls through to a fresh run.
func resumeApproves(reader *bufio.Reader, out io.Writer, st State) bool {
	pass, pending, blocked := st.Summary()
	fmt.Fprintf(out, "\nResumable run found: %d done, %d pending, %d blocked.\n", pass, pending, blocked)
	fmt.Fprint(out, "Type 'resume' to continue, anything else to restart: ")
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line) == "resume"
}
