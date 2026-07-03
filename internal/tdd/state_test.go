package tdd

import (
	"path/filepath"
	"testing"
)

func TestBeginRunAllPending(t *testing.T) {
	st := BeginRun("do things", "feat/x", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	if st.Version != StateVersion || st.Task != "do things" || st.Branch != "feat/x" {
		t.Fatalf("meta wrong: %+v", st)
	}
	if len(st.Features) != 2 || st.Features[0].Status != "pending" || st.Features[1].Status != "pending" {
		t.Fatalf("features not all pending: %+v", st.Features)
	}
}

func TestNextPendingAndMark(t *testing.T) {
	st := BeginRun("t", "", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	n, ok := st.NextPending()
	if !ok || n != "a" {
		t.Fatalf("first pending = %q,%v", n, ok)
	}
	st.Mark("a", "pass")
	n, ok = st.NextPending()
	if !ok || n != "b" {
		t.Fatalf("second pending = %q,%v", n, ok)
	}
	st.Mark("b", "blocked")
	if _, ok := st.NextPending(); ok {
		t.Fatalf("expected no pending left")
	}
}

func TestResumableAndSummary(t *testing.T) {
	st := BeginRun("t", "", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	st.Mark("a", "pass")
	if !st.Resumable() {
		t.Fatalf("one pending should be resumable")
	}
	pass, pending, blocked := st.Summary()
	if pass != 1 || pending != 1 || blocked != 0 {
		t.Fatalf("summary = %d,%d,%d", pass, pending, blocked)
	}
	st.Mark("b", "blocked")
	if st.Resumable() {
		t.Fatalf("no pending should not be resumable")
	}
	if (State{}).Resumable() {
		t.Fatalf("empty state must not be resumable")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	want := BeginRun("count", "feat/count", []FeaturePlan{{Name: "a"}})
	want.Mark("a", "pass")
	if err := SaveState(p, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Task != want.Task || len(got.Features) != 1 || got.Features[0].Status != "pass" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestLoadStateMissing(t *testing.T) {
	got, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should be nil error, got %v", err)
	}
	if len(got.Features) != 0 || got.Resumable() {
		t.Fatalf("missing file should be zero State, got %+v", got)
	}
}

func TestScenarioStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st := BeginRun("task", "branch", []FeaturePlan{{Name: "feat-a"}})
	st.SetScenario("feat-a", ScenarioState{Tag: "@s1", Phase: "green", Kind: "tdd"})
	st.SetScenario("feat-a", ScenarioState{Tag: "@s1", Phase: "done", Kind: "tdd"}) // upsert
	st.SetScenario("feat-a", ScenarioState{Tag: "@s2", Phase: "red", Kind: "regression"})
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	var fs *FeatureState
	for i := range got.Features {
		if got.Features[i].Name == "feat-a" {
			fs = &got.Features[i]
		}
	}
	if fs == nil || len(fs.Scenarios) != 2 {
		t.Fatalf("scenarios = %+v, want 2", fs)
	}
	if fs.Scenarios[0].Tag != "@s1" || fs.Scenarios[0].Phase != "done" {
		t.Fatalf("s1 = %+v, want @s1/done", fs.Scenarios[0])
	}
}

// TestBeginRunPersistsKind proves FeaturePlan.Kind survives BeginRun and a
// save/load round trip, and that Feature() looks it up by name.
func TestBeginRunPersistsKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st := BeginRun("t", "b", []FeaturePlan{{Name: "a", Kind: "refactor"}, {Name: "b"}})
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	fa, ok := got.Feature("a")
	if !ok || fa.Kind != "refactor" {
		t.Fatalf("Feature(a) = %+v, ok=%v, want Kind=refactor", fa, ok)
	}
	fb, ok := got.Feature("b")
	if !ok || fb.Kind != "" {
		t.Fatalf("Feature(b) = %+v, ok=%v, want Kind=\"\"", fb, ok)
	}
	if _, ok := got.Feature("nope"); ok {
		t.Fatalf("Feature(nope) should not be found")
	}
}
