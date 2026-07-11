package tdd

import (
	"os"
	"path/filepath"
	"testing"
)

// @s1 — State round-trips the review field, and a legacy state.json written
// without any review field loads with review empty (so old runs never gain
// review semantics just because the field was absent).

// TestReviewFieldRoundTrip proves State.Review survives a SaveState/LoadState
// round trip.
func TestReviewFieldRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st, err := BeginRun("t", "b", []FeaturePlan{{Name: "a"}})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	st.Review = "pending"
	if err := SaveState(p, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Review != "pending" {
		t.Fatalf("Review = %q, want %q", got.Review, "pending")
	}
}

// TestLoadStateLegacyNoReviewField loads a state.json written by an older
// binary (no review key at all) and asserts Review is empty — legacy files are
// never silently promoted into a review-tracked run.
func TestLoadStateLegacyNoReviewField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	raw := `{
		"version": 1,
		"task": "t",
		"branch": "b",
		"features": [{"name": "a", "status": "pass"}]
	}`
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Review != "" {
		t.Fatalf("legacy Review = %q, want empty string", got.Review)
	}
}

// @s2 — Resumable semantics for the review field: a fully-passed run is
// resumable only when review is "pending"; "", "pass" and "skipped" are not
// resumable. Existing pending-feature semantics stay intact.
func TestResumableReviewSemantics(t *testing.T) {
	allPass := func(review string) State {
		return State{
			Version: StateVersion,
			Features: []FeatureState{
				{Name: "a", Status: "pass"},
				{Name: "b", Status: "pass"},
			},
			Review: review,
		}
	}
	tests := []struct {
		name string
		st   State
		want bool
	}{
		{"all pass + review pending -> resumable", allPass("pending"), true},
		{"all pass + review empty (legacy) -> not resumable", allPass(""), false},
		{"all pass + review pass -> not resumable", allPass("pass"), false},
		{"all pass + review skipped -> not resumable", allPass("skipped"), false},
		{
			"pending feature still resumable regardless of review",
			State{Version: StateVersion, Features: []FeatureState{{Name: "a", Status: "pending"}}},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.Resumable(); got != tc.want {
				t.Fatalf("Resumable() = %v, want %v", got, tc.want)
			}
		})
	}
}
