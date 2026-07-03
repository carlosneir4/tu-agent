package tdd

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/testresult"
)

// scriptDispatcher returns queued outputs in order, recording the tasks it saw.
type scriptDispatcher struct {
	outs  []string
	tasks []string
	i     int
}

func (s *scriptDispatcher) Dispatch(_ context.Context, agent, task string) (string, error) {
	s.tasks = append(s.tasks, agent+"|"+task)
	o := s.outs[s.i]
	s.i++
	return o, nil
}

func contract(scenarios ...string) string {
	body := `{"stage":"x","status":"pass","scenarios":[`
	for i, sc := range scenarios {
		if i > 0 {
			body += ","
		}
		body += `"` + sc + `"`
	}
	body += `],"artifacts":[{"kind":"test","path":"src/test/java/com/acme/FooTest.java"}]}`
	return "done\n```json\n" + body + "\n```"
}

// contractWithSource returns a scripted contract carrying a kind:"source"
// artifact at path, for tests that exercise the mutation target derived from
// the implementer's contract on the sandwich path.
func contractWithSource(path string, scenarios ...string) string {
	body := `{"stage":"x","status":"pass","scenarios":[`
	for i, sc := range scenarios {
		if i > 0 {
			body += ","
		}
		body += `"` + sc + `"`
	}
	body += `],"artifacts":[{"kind":"source","path":"` + path + `"}]}`
	return "done\n```json\n" + body + "\n```"
}

// TestRunSandwichReturnsSourceArtifact proves the happy-path SandwichResult
// carries the implementer's source artifact, so the conductor can derive a
// mutation target from it — the sandwich path never returns a craftsman
// Contract to the caller directly.
func TestRunSandwichReturnsSourceArtifact(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer
		contractWithSource("src/main/java/com/acme/Foo.java", "@s1"), // implementer
	}}
	// Snapshots: before, after-tests (RED loop), fresh GREEN baseline, after-impl.
	snaps := []string{"S0", "S1", "S1b", "S2"}
	var si int
	var runN int
	deps := SandwichDeps{
		Runner: StageRunner{D: disp},
		Tests: func(_ context.Context) (bool, string, error) {
			runN++
			return runN >= 2, "", nil // red first, green second
		},
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" { // test-writer changes: a test file only
				return []string{"core/src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil // implementer: prod only
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{
				{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail},
			}}, nil
		},
		Out: &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1"}, []string{"@s1"})
	if !res.OK {
		t.Fatalf("sandwich failed: %+v", res)
	}
	if res.SourceArtifact.Path != "src/main/java/com/acme/Foo.java" {
		t.Fatalf("SourceArtifact.Path = %q, want %q", res.SourceArtifact.Path, "src/main/java/com/acme/Foo.java")
	}
}

func TestRunSandwichHappyPath(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer
		contract("@s1"), // implementer
	}}
	// Snapshots: before, after-tests (RED loop), fresh GREEN baseline, after-impl.
	snaps := []string{"S0", "S1", "S1b", "S2"}
	var si int
	// Test runner: first call (RED phase) fails; second (GREEN) passes.
	var runN int
	deps := SandwichDeps{
		Runner: StageRunner{D: disp},
		Tests: func(_ context.Context) (bool, string, error) {
			runN++
			return runN >= 2, "", nil // red first, green second
		},
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, from, to string) ([]string, error) {
			if from == "S0" { // test-writer changes: a test file only
				return []string{"core/src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil // implementer: prod only
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{
				{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail},
			}}, nil
		},
		Out: &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1"}, []string{"@s1"})
	if !res.OK {
		t.Fatalf("sandwich failed: %+v", res)
	}
}

// TestRunSandwichGreenOnArrivalReturnsTags proves the green-on-arrival branch
// of runSandwichCycle reports Regressions as the @s TAGS the batch was called
// with, not the underlying test file paths — ScenarioState.SetScenario keys
// on tags, so file paths would silently break the wiring.
func TestRunSandwichGreenOnArrivalReturnsTags(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1", "@s2"), // test-writer only — green-on-arrival skips the GREEN phase
	}}
	snaps := []string{"S0", "S1"}
	var si int
	deps := SandwichDeps{
		Runner: StageRunner{D: disp},
		Tests: func(_ context.Context) (bool, string, error) {
			return false, "", nil // suite still red overall (something else failing)
		},
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"src/test/java/com/acme/FooTest.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{
				{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass},
			}}, nil
		},
		Out: &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1", "@s2"}, []string{"@s1", "@s2"})
	if !res.OK {
		t.Fatalf("expected green-on-arrival to be accepted as OK: %+v", res)
	}
	want := []string{"@s1", "@s2"}
	if !slices.Equal(res.Regressions, want) {
		t.Fatalf("Regressions = %v, want %v (tags, not file paths)", res.Regressions, want)
	}
}

func TestRunSandwichStrict(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer, scenario 1
		contract("@s1"), // implementer, scenario 1
		contract("@s2"), // test-writer, scenario 2
		contract("@s2"), // implementer, scenario 2
	}}
	// Snapshots: before/after-tests(RED loop)/fresh-GREEN-baseline/after-impl per
	// scenario, 2 scenarios.
	snaps := []string{"S0", "S1", "S1b", "S2", "S3", "S4", "S4b", "S5"}
	var si int
	// Diff calls alternate RED (test file only), GREEN (prod file only), per
	// scenario, regardless of the snapshot ids involved.
	var diffN int
	// Tests alternate red (RED phase), green (GREEN phase), per scenario.
	var runN int
	deps := SandwichDeps{
		Strict: true,
		Runner: StageRunner{D: disp},
		Tests: func(_ context.Context) (bool, string, error) {
			passed := runN%2 == 1
			runN++
			return passed, "", nil
		},
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			defer func() { diffN++ }()
			if diffN%2 == 0 { // RED diff: test-writer changes, a test file only
				return []string{"core/src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil // GREEN diff: prod only
		},
		LoadReports: func(time.Time) (testresult.Report, error) { return testresult.Report{}, nil },
		Out:         &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1", "@s2"}, []string{"@s1", "@s2"})
	if !res.OK {
		t.Fatalf("strict sandwich failed: %+v", res)
	}
	if len(disp.tasks) != 4 {
		t.Fatalf("expected 4 dispatches (test-writer+implementer per scenario), got %d: %v", len(disp.tasks), disp.tasks)
	}
	wantAgents := []string{"craftsman", "craftsman", "craftsman", "craftsman"}
	for i, want := range wantAgents {
		if !strings.HasPrefix(disp.tasks[i], want+"|") {
			t.Errorf("dispatch %d: got %q, want agent %q", i, disp.tasks[i], want)
		}
	}
	if !strings.Contains(disp.tasks[0], "@s1") || strings.Contains(disp.tasks[0], "@s2") {
		t.Errorf("dispatch 0 (scenario 1 test-writer) should scope to @s1 only: %q", disp.tasks[0])
	}
	if !strings.Contains(disp.tasks[2], "@s2") || strings.Contains(disp.tasks[2], "@s1") {
		t.Errorf("dispatch 2 (scenario 2 test-writer) should scope to @s2 only: %q", disp.tasks[2])
	}
}

func TestRunSandwichRejectsTestWriterProduction(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{contract("@s1")}}
	snaps := []string{"S0", "S1"}
	var si int
	deps := SandwichDeps{
		Runner:   StageRunner{D: disp},
		Tests:    func(_ context.Context) (bool, string, error) { return false, "", nil },
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) { return testresult.Report{}, nil },
		Out:         &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1"}, []string{"@s1"})
	if res.OK {
		t.Fatal("expected rejection: test-writer wrote production")
	}
}

func TestRefineSandwich(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{contract("@s1")}}
	snaps := []string{"S0", "S1"}
	var si int
	deps := SandwichDeps{
		Runner:   StageRunner{D: disp},
		Tests:    func(_ context.Context) (bool, string, error) { return true, "", nil },
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"src/main/java/com/acme/Foo.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) { return testresult.Report{}, nil },
		Out:         &bytes.Buffer{},
	}
	res := RefineSandwich(context.Background(), deps, "feat", []string{"@s1"}, "tighten the naming")
	if !res.OK {
		t.Fatalf("refine failed: %+v", res)
	}
	if len(disp.tasks) != 1 {
		t.Fatalf("expected 1 dispatch (implementer only, no RED phase), got %d: %v", len(disp.tasks), disp.tasks)
	}
	if !strings.HasPrefix(disp.tasks[0], "craftsman|") || !strings.Contains(disp.tasks[0], "tighten the naming") {
		t.Errorf("dispatch 0 should be the implementer carrying the feedback: %q", disp.tasks[0])
	}
}

func TestRefineSandwichRejectsTestFileTouch(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{contract("@s1")}}
	snaps := []string{"S0", "S1"}
	var si int
	deps := SandwichDeps{
		Runner:   StageRunner{D: disp},
		Tests:    func(_ context.Context) (bool, string, error) { return true, "", nil },
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"core/src/test/java/com/acme/FooTest.java"}, nil
		},
		LoadReports: func(time.Time) (testresult.Report, error) { return testresult.Report{}, nil },
		Out:         &bytes.Buffer{},
	}
	res := RefineSandwich(context.Background(), deps, "feat", []string{"@s1"}, "tighten the naming")
	if res.OK {
		t.Fatal("expected rejection: refine touched a test file")
	}
	if !strings.Contains(res.Feedback, "modified test files") {
		t.Fatalf("feedback = %q, want it to mention modified test files", res.Feedback)
	}
}

// TestRunRefactor covers the refactor escape hatch: no RED phase, just a
// refactor dispatch followed by a deterministic gate against the whole suite.
func TestRunRefactor(t *testing.T) {
	t.Run("green first attempt", func(t *testing.T) {
		disp := &scriptDispatcher{outs: []string{contract("@s1")}} // refactor dispatch only
		deps := SandwichDeps{
			Runner: StageRunner{D: disp},
			Tests:  func(_ context.Context) (bool, string, error) { return true, "", nil },
			Out:    &bytes.Buffer{},
		}
		res := RunRefactor(context.Background(), deps, "feat", []string{"@s1"}, "")
		if !res.OK {
			t.Fatalf("expected refactor to succeed: %+v", res)
		}
		if len(res.Regressions) != 0 {
			t.Fatalf("refactor must never report Regressions: %+v", res.Regressions)
		}
		if len(disp.tasks) != 1 {
			t.Fatalf("expected exactly 1 dispatch (refactor, no test-writer), got %d: %v", len(disp.tasks), disp.tasks)
		}
		if !strings.HasPrefix(disp.tasks[0], "craftsman|") || !strings.Contains(disp.tasks[0], RefactorPrompt) {
			t.Errorf("dispatch 0 should be the refactor prompt: %q", disp.tasks[0][:min(60, len(disp.tasks[0]))])
		}
	})

	t.Run("retries then green", func(t *testing.T) {
		disp := &scriptDispatcher{outs: []string{contract("@s1"), contract("@s1")}}
		var runN int
		deps := SandwichDeps{
			Budget: 2,
			Runner: StageRunner{D: disp},
			Tests: func(_ context.Context) (bool, string, error) {
				runN++
				return runN >= 2, "", nil // red first attempt, green second
			},
			Out: &bytes.Buffer{},
		}
		res := RunRefactor(context.Background(), deps, "feat", []string{"@s1"}, "")
		if !res.OK {
			t.Fatalf("expected refactor to eventually succeed: %+v", res)
		}
		if len(disp.tasks) != 2 {
			t.Fatalf("expected 2 dispatches (retry), got %d: %v", len(disp.tasks), disp.tasks)
		}
	})

	t.Run("stays red", func(t *testing.T) {
		disp := &scriptDispatcher{outs: []string{contract("@s1"), contract("@s1")}}
		deps := SandwichDeps{
			Budget: 2,
			Runner: StageRunner{D: disp},
			Tests:  func(_ context.Context) (bool, string, error) { return false, "", nil },
			Out:    &bytes.Buffer{},
		}
		res := RunRefactor(context.Background(), deps, "feat", []string{"@s1"}, "")
		if res.OK {
			t.Fatal("expected refactor to fail: suite stayed red")
		}
		if !strings.Contains(res.Feedback, "refactor left the suite red after retries") {
			t.Fatalf("feedback = %q, want it to mention the suite stayed red", res.Feedback)
		}
	})
}

// TestRunSandwichRetriesGreen: the implementer's first attempt leaves the
// suite red; the GREEN phase must retry in place (no re-run of RED, no
// re-dispatch of the test-writer) and succeed on the second attempt.
func TestRunSandwichRetriesGreen(t *testing.T) {
	disp := &scriptDispatcher{outs: []string{
		contract("@s1"), // test-writer (RED, succeeds first try)
		contract("@s1"), // implementer, attempt 1 (leaves suite red)
		contract("@s1"), // implementer, attempt 2 (green)
	}}
	// Snapshots: before, after-tests(RED), fresh GREEN baseline, after-impl x2.
	snaps := []string{"S0", "S1", "S1b", "S2", "S3"}
	var si int
	// Tests(): call 1 = RED phase check (must be red); call 2 = GREEN attempt 1
	// (still red); call 3 = GREEN attempt 2 (green).
	var runN int
	deps := SandwichDeps{
		Budget: 2,
		Runner: StageRunner{D: disp},
		Tests: func(_ context.Context) (bool, string, error) {
			runN++
			return runN >= 3, "", nil
		},
		Snapshot: func(context.Context) (string, error) { s := snaps[si]; si++; return s, nil },
		Diff: func(_ context.Context, from, _ string) ([]string, error) {
			if from == "S0" { // test-writer changes: a test file only
				return []string{"core/src/test/java/com/acme/FooTest.java"}, nil
			}
			return []string{"src/main/java/com/acme/Foo.java"}, nil // implementer: prod only
		},
		LoadReports: func(time.Time) (testresult.Report, error) {
			return testresult.Report{Cases: []testresult.Case{
				{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail},
			}}, nil
		},
		Out: &bytes.Buffer{},
	}
	res := RunSandwich(context.Background(), deps, "feat", []string{"@s1"}, []string{"@s1"})
	if !res.OK {
		t.Fatalf("sandwich failed: %+v", res)
	}
	if len(disp.tasks) != 3 {
		t.Fatalf("expected 3 dispatches (1 test-writer + 2 implementer attempts), got %d: %v", len(disp.tasks), disp.tasks)
	}
	if !strings.HasPrefix(disp.tasks[0], "craftsman|") || !strings.Contains(disp.tasks[0], TestWriterPrompt) {
		t.Errorf("dispatch 0 should be the test-writer: %q", disp.tasks[0][:min(60, len(disp.tasks[0]))])
	}
	if !strings.Contains(disp.tasks[2], "previous attempt was rejected") {
		t.Errorf("dispatch 2 (implementer retry) should carry GREEN-phase feedback: %q", disp.tasks[2])
	}
}
