package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeginRunAllPending(t *testing.T) {
	st, err := BeginRun("do things", "feat/x", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if st.Version != StateVersion || st.Task != "do things" || st.Branch != "feat/x" {
		t.Fatalf("meta wrong: %+v", st)
	}
	if len(st.Features) != 2 || st.Features[0].Status != "pending" || st.Features[1].Status != "pending" {
		t.Fatalf("features not all pending: %+v", st.Features)
	}
}

func TestBeginRunRejectsDuplicates(t *testing.T) {
	_, err := BeginRun("t", "b", []FeaturePlan{{Name: "x"}, {Name: "x"}})
	if err == nil || !strings.Contains(err.Error(), "duplicate feature") {
		t.Fatalf("want duplicate-feature error, got %v", err)
	}
}

func TestNextPendingAndMark(t *testing.T) {
	st, err := BeginRun("t", "", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
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
	st, err := BeginRun("t", "", []FeaturePlan{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
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
	want, err := BeginRun("count", "feat/count", []FeaturePlan{{Name: "a"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
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
	st, err := BeginRun("task", "branch", []FeaturePlan{{Name: "feat-a"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
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
	st, err := BeginRun("t", "b", []FeaturePlan{{Name: "a", Kind: "refactor"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
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

// TestLoadStateDedupesDuplicateFeatures reproduces a resume against a
// state.json written by an older binary (or hand-edited) that contains two
// features with the same name. BeginRun's duplicate guard only protects the
// construction path — LoadState must defensively dedupe (keep-first) so a
// stale/hand-edited file on disk can't wedge NextPending/Mark into an
// infinite loop on resume. This must NOT error: resume must not brick an
// existing run.
func TestLoadStateDedupesDuplicateFeatures(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	raw := `{
		"version": 1,
		"task": "t",
		"branch": "b",
		"features": [
			{"name": "x", "status": "pending"},
			{"name": "x", "status": "pending"},
			{"name": "y", "status": "pending"}
		]
	}`
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := LoadState(p)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(st.Features) != 2 {
		t.Fatalf("want 2 deduped features, got %d: %+v", len(st.Features), st.Features)
	}

	// A NextPending -> Mark("x", "pass") -> NextPending cycle must terminate:
	// the second call must not return "x" again.
	name, ok := st.NextPending()
	if !ok || name != "x" {
		t.Fatalf("first pending = %q,%v, want x", name, ok)
	}
	st.Mark(name, "pass")
	name, ok = st.NextPending()
	if !ok || name != "y" {
		t.Fatalf("second pending = %q,%v, want y (loop over x would return x again)", name, ok)
	}
}
