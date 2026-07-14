package advise

import "testing"

func TestParseState_EmptyStringIsEmptyState(t *testing.T) {
	st, err := ParseState("")
	if err != nil {
		t.Fatalf("ParseState(\"\") error = %v", err)
	}
	if st.Rules == nil {
		t.Fatal("ParseState(\"\") Rules is nil, want empty map")
	}
	if len(st.Rules) != 0 {
		t.Fatalf("ParseState(\"\") Rules = %+v, want empty", st.Rules)
	}
}

func TestParseState_Malformed(t *testing.T) {
	if _, err := ParseState("{not json"); err == nil {
		t.Fatal("ParseState with malformed JSON: want error, got nil")
	}
}

func TestParseState_RoundTrip(t *testing.T) {
	want := State{Rules: map[string]RuleState{
		"crystallize-ready":    {LastShownEvidence: 2},
		"edit-without-context": {LastShownEvidence: 5, Dismissed: true},
	}}
	raw, err := want.Marshal()
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	got, err := ParseState(raw)
	if err != nil {
		t.Fatalf("ParseState(%q) error = %v", raw, err)
	}
	if len(got.Rules) != len(want.Rules) {
		t.Fatalf("got.Rules = %+v, want %+v", got.Rules, want.Rules)
	}
	for id, rs := range want.Rules {
		if got.Rules[id] != rs {
			t.Errorf("Rules[%q] = %+v, want %+v", id, got.Rules[id], rs)
		}
	}
}

func TestFilter_ShowsOnlyOnEvidenceGrowth(t *testing.T) {
	suggestions := []Suggestion{
		{RuleID: "crystallize-ready", Evidence: 3},
	}
	st := State{Rules: map[string]RuleState{
		"crystallize-ready": {LastShownEvidence: 3},
	}}
	show, next := Filter(suggestions, st, 2)
	if len(show) != 0 {
		t.Errorf("show = %+v, want empty (no evidence growth)", show)
	}
	if next.Rules["crystallize-ready"].LastShownEvidence != 3 {
		t.Errorf("next state LastShownEvidence changed unexpectedly: %+v", next.Rules["crystallize-ready"])
	}

	suggestions[0].Evidence = 4
	show, next = Filter(suggestions, st, 2)
	if len(show) != 1 {
		t.Fatalf("show = %+v, want 1 suggestion (evidence grew 3->4)", show)
	}
	if next.Rules["crystallize-ready"].LastShownEvidence != 4 {
		t.Errorf("next state LastShownEvidence = %d, want 4", next.Rules["crystallize-ready"].LastShownEvidence)
	}
}

func TestFilter_RespectsDismissed(t *testing.T) {
	suggestions := []Suggestion{
		{RuleID: "secret-guard", Evidence: 10},
	}
	st := State{Rules: map[string]RuleState{
		"secret-guard": {Dismissed: true},
	}}
	show, next := Filter(suggestions, st, 2)
	if len(show) != 0 {
		t.Errorf("show = %+v, want empty (dismissed)", show)
	}
	if !next.Rules["secret-guard"].Dismissed {
		t.Error("next state lost Dismissed=true")
	}
}

func TestFilter_CapsAtBudgetKeepingOrder(t *testing.T) {
	suggestions := []Suggestion{
		{RuleID: "crystallize-ready", Evidence: 5},
		{RuleID: "edit-without-context", Evidence: 4},
		{RuleID: "secret-guard", Evidence: 3},
	}
	st := State{Rules: map[string]RuleState{}}
	show, next := Filter(suggestions, st, 2)
	if len(show) != 2 {
		t.Fatalf("show = %+v, want 2 (budget cap)", show)
	}
	if show[0].RuleID != "crystallize-ready" || show[1].RuleID != "edit-without-context" {
		t.Errorf("show = %+v, want input order preserved", show)
	}
	// The third (over-budget) rule must NOT be bumped: it stays eligible
	// next run at the same evidence.
	if rs, ok := next.Rules["secret-guard"]; ok && rs.LastShownEvidence != 0 {
		t.Errorf("over-budget rule was bumped: %+v", rs)
	}
	if next.Rules["crystallize-ready"].LastShownEvidence != 5 {
		t.Errorf("shown rule not bumped: %+v", next.Rules["crystallize-ready"])
	}
	if next.Rules["edit-without-context"].LastShownEvidence != 4 {
		t.Errorf("shown rule not bumped: %+v", next.Rules["edit-without-context"])
	}
}

func TestFilter_EmptyStatePersistsBaseline(t *testing.T) {
	suggestions := []Suggestion{{RuleID: "crystallize-ready", Evidence: 1}}
	show, next := Filter(suggestions, State{Rules: map[string]RuleState{}}, 2)
	if len(show) != 1 {
		t.Fatalf("show = %+v, want 1 (first-ever nudge)", show)
	}
	if next.Rules["crystallize-ready"].LastShownEvidence != 1 {
		t.Errorf("next state = %+v, want LastShownEvidence 1", next.Rules["crystallize-ready"])
	}
}
