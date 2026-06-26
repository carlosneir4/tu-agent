package tdd

import (
	"path/filepath"
	"testing"
)

func TestBeginRunAllPending(t *testing.T) {
	st := BeginRun("do things", "feat/x", []string{"a", "b"})
	if st.Version != StateVersion || st.Task != "do things" || st.Branch != "feat/x" {
		t.Fatalf("meta wrong: %+v", st)
	}
	if len(st.Features) != 2 || st.Features[0].Status != "pending" || st.Features[1].Status != "pending" {
		t.Fatalf("features not all pending: %+v", st.Features)
	}
}

func TestNextPendingAndMark(t *testing.T) {
	st := BeginRun("t", "", []string{"a", "b"})
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
	st := BeginRun("t", "", []string{"a", "b"})
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
	want := BeginRun("count", "feat/count", []string{"a"})
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
