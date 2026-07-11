package tdd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tu/tu-agent/internal/testresult"
)

// Options configures one flow run.
type Options struct {
	Analyst    Chatter
	Dispatcher Dispatcher
	Runner     TestRunner
	In         io.Reader
	Out        io.Writer
	Task       string
	Branch     string
	WorkDir    string
	// RelBase is the repo-relative per-feature artifact dir (e.g.
	// .tu-agent/tdd/<ticket>-<slug>) — the same dir the stage overlays reference.
	RelBase           string
	FeatureReader     func(name string) (string, error)
	Budget            int
	Mutator           Mutator
	MutationThreshold float64
	Archive           bool
	// Strict opts the sandwich into the per-scenario RED->GREEN loop instead of
	// batching a sub-feature's scenarios. Default false.
	Strict bool

	// DesignDoc, when set, is a design doc or superpowers plan the analyst seeds
	// the spec from (confirm-by-exception) instead of interrogating from zero.
	DesignDoc string

	// Sandwich deps enable the RED->GREEN enforcement path. All three must be
	// non-nil to enable it; otherwise runFeatureTDD uses the single-dispatch path.
	Snapshot    func(ctx context.Context) (string, error)
	Diff        func(ctx context.Context, from, to string) ([]string, error)
	LoadReports func(since time.Time) (testresult.Report, error)

	// ReviewScope computes the post-loop whole-branch review scope: the
	// merge-base against the default branch and the files changed since it, or a
	// non-empty skipReason when the branch is unscopable. Injected on Options
	// (like Snapshot/Diff) so flow tests stay fake-based; defaults to the real
	// ReviewScope when nil.
	ReviewScope func(ctx context.Context, root string) (base string, files []string, skipReason string, err error)
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
	if o.ReviewScope == nil {
		// The default is rooted at o.WorkDir (the .tu-agent/tdd/<slug> artifact
		// dir), not the repo root. It works only because the real ReviewScope
		// shells out to git, which walks up from any subdirectory to find the
		// enclosing repo. The CLI caller passes the repo root explicitly; this
		// default relies on that walk-up.
		o.ReviewScope = ReviewScope
	}
	reader := bufio.NewReader(o.In)
	runner := StageRunner{D: o.Dispatcher}
	statePath := filepath.Join(o.WorkDir, "state.json")

	st, err := LoadState(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: tdd state unreadable (%v) — starting fresh\n", err)
	}
	resume := st.Resumable() && resumeApproves(reader, o.Out, st)

	if !resume {
		analystTask := o.Task
		if o.DesignDoc != "" {
			analystTask = "A design doc or superpowers plan exists at " + o.DesignDoc +
				" — read it and seed the spec from it, confirming by exception (ask only about gaps, ambiguities, or contradictions).\n\n" + o.Task
		}
		if _, err := RunAnalyst(ctx, o.Analyst, analystTask, reader, o.Out); err != nil {
			return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
		}
		var feats []FeaturePlan
		archFeedback := ""
		approved := false
		// Design loop: at most 3 architect→human-gate rounds (revise feedback is
		// fed back to the architect); not user-configurable yet.
		for designBudget := 3; designBudget > 0; designBudget-- {
			base := o.RelBase
			if base == "" {
				base = ".tu-agent/tdd"
			}
			task := "Read " + base + "/spec.md and produce the design, scenarios, and complexity classification."
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
			var dups []string
			feats, dups = planFeatures(arch)
			for _, d := range dups {
				fmt.Fprintf(o.Out, "warning: architect emitted duplicate feature %q — keeping first\n", d)
			}
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
		var err error
		st, err = BeginRun(o.Task, o.Branch, feats)
		if err != nil {
			return Result{Status: StatusBlocked}, fmt.Errorf("tdd.Run: %w", err)
		}
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
		kind := ""
		if fs, ok := st.Feature(name); ok {
			kind = fs.Kind
		}
		status, scenarios, ferr := runFeatureTDD(ctx, o, runner, name, kind, ScenarioTags(featSrc))
		if ferr != nil {
			return Result{Status: StatusBlocked, Features: st.Features}, fmt.Errorf("tdd.Run: %w", ferr)
		}
		for _, sc := range scenarios {
			st.SetScenario(name, sc)
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
	// Whole-branch review runs once, only when every feature passed (never on
	// the trivial early-return path, never when a feature blocked). It never
	// changes the run result — features already passed their gates.
	if overall == StatusPass && pass > 0 {
		if err := runPostLoopReview(ctx, o, runner, &st, statePath); err != nil {
			return Result{Status: StatusBlocked, Features: st.Features}, fmt.Errorf("tdd.Run: %w", err)
		}
	}
	return Result{Status: overall, Features: st.Features}, nil
}

// runFeatureTDD runs the standard TDD loop for one feature, returning its
// terminal status and the per-scenario states observed along the way (nil on
// the non-sandwich path or on an early error). A nil error with StatusBlocked
// means a logical failure (budget/fail); a non-nil error means a stage
// dispatch could not run.
func runFeatureTDD(ctx context.Context, o Options, runner StageRunner, feature, kind string, featureTags []string) (string, []ScenarioState, error) {
	feedback := ""
	sandwichEnabled := o.Snapshot != nil && o.Diff != nil && o.LoadReports != nil
	greenAchieved := false
	isRefactor := kind == "refactor"
	var craft Contract
	var scenarios []ScenarioState
	var regressions []string
	var sourceArtifact Artifact
	for budget := o.Budget; budget > 0; budget-- {
		if sandwichEnabled {
			deps := SandwichDeps{
				Runner:      runner,
				Tests:       o.Runner,
				Snapshot:    o.Snapshot,
				Diff:        o.Diff,
				LoadReports: o.LoadReports,
				Out:         o.Out,
				Strict:      o.Strict,
				Budget:      o.Budget,
			}
			// The craftsman self-reports covered scenarios via the implementer
			// contract; here featureTags double as the covered set because the
			// gate already proved each test red then green.
			var sw SandwichResult
			switch {
			case isRefactor:
				sw = RunRefactor(ctx, deps, feature, featureTags, feedback)
			case !greenAchieved:
				sw = RunSandwich(ctx, deps, feature, featureTags, featureTags)
			default:
				sw = RefineSandwich(ctx, deps, feature, featureTags, feedback)
			}
			if !sw.OK {
				// Phases (and refine) already retried internally, so a failure
				// here is terminal — re-running from scratch would either dead-
				// end (tests already exist) or mask the rejection.
				fmt.Fprintf(o.Out, "sandwich: %s\n", sw.Feedback)
				return StatusBlocked, scenarios, nil
			}
			if scenarios == nil {
				// Record per-scenario kind only the first time green is achieved —
				// a later refine pass revises production, not scenario coverage.
				if isRefactor {
					scenarios = make([]ScenarioState, 0, len(featureTags))
					for _, tag := range featureTags {
						scenarios = append(scenarios, ScenarioState{Tag: tag, Phase: "done", Kind: "refactor"})
					}
				} else {
					regressions = sw.Regressions
					regSet := make(map[string]bool, len(regressions))
					for _, t := range regressions {
						regSet[t] = true
					}
					scenarios = make([]ScenarioState, 0, len(featureTags))
					for _, tag := range featureTags {
						scKind := "tdd"
						if regSet[tag] {
							scKind = "regression"
						}
						scenarios = append(scenarios, ScenarioState{Tag: tag, Phase: "done", Kind: scKind})
					}
				}
			}
			greenAchieved = true
			if sw.SourceArtifact.Path != "" {
				sourceArtifact = sw.SourceArtifact
			}
		} else {
			task := "Implement the approved feature by strict TDD. handoff: " + feature
			if feedback != "" {
				task += "\n\nJudge feedback to address:\n" + feedback
			}
			var err error
			craft, err = runner.Run(ctx, "craftsman", task)
			if err != nil {
				return StatusBlocked, scenarios, err
			}
			if det := DeterministicJudge(ctx, o.Runner, featureTags, craft.Scenarios); !det.OK {
				feedback = det.Feedback
				fmt.Fprintf(o.Out, "deterministic gate: %s (budget %d)\n", det.Feedback, budget-1)
				continue
			}
		}
		judgeTask := "Judge design and discipline for feature " + feature + ". The deterministic gate already passed."
		if isRefactor {
			judgeTask += "\n\nNote: this is a refactor (no new behavior or tests) — evaluate structure/design only; do not credit it as a TDD victory."
		} else if len(regressions) > 0 {
			judgeTask += "\n\nNote: these scenarios were green-on-arrival regression coverage, not RED→GREEN — do not credit them as TDD victories: " + strings.Join(regressions, ", ")
		}
		judge, err := runner.Run(ctx, "judge", judgeTask)
		if err != nil {
			return StatusBlocked, scenarios, err
		}
		if judge.Verdict == nil {
			return StatusBlocked, scenarios, fmt.Errorf("judge returned no verdict for %s", feature)
		}
		switch judge.Verdict.Result {
		case "pass":
			if o.Mutator != nil && !isRefactor {
				var mt MutationTarget
				var ok bool
				if sandwichEnabled {
					mt, ok = MutationTargetFromArtifact(sourceArtifact)
				} else {
					mt, ok = MutationTargetFromContract(craft)
				}
				if ok {
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
			return StatusPass, scenarios, nil
		case "revise":
			feedback = judge.Verdict.Feedback
			fmt.Fprintf(o.Out, "judge: revise — %s (budget %d)\n", feedback, budget-1)
		default: // "fail"
			return StatusBlocked, scenarios, nil
		}
	}
	fmt.Fprintf(o.Out, "retry budget exhausted — feature %s blocked.\n", feature)
	return StatusBlocked, scenarios, nil
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
