package advise

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

func TestEvaluate_CrystallizeReady(t *testing.T) {
	cases := []struct {
		name  string
		needs int
		fires bool
	}{
		{"zero needs does not fire", 0, false},
		{"one need fires", 1, true},
		{"several needs fires", 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(Inputs{CrystallizeNeeds: tc.needs})
			found := false
			for _, s := range got {
				if s.RuleID == "crystallize-ready" {
					found = true
					if s.Evidence != tc.needs {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.needs)
					}
					if !strings.Contains(s.Message, "tu-agent memory crystallize") {
						t.Errorf("Message = %q, want it to mention `tu-agent memory crystallize`", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("crystallize-ready fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_EditWithoutContext(t *testing.T) {
	cases := []struct {
		name  string
		count int
		fires bool
	}{
		{"below threshold", evidenceThreshold - 1, false},
		{"at threshold", evidenceThreshold, true},
		{"above threshold", evidenceThreshold + 5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Inputs{Insights: stats.InsightsSummary{Violations: map[string]int{"edit-without-context": tc.count}}}
			got := Evaluate(in)
			found := false
			for _, s := range got {
				if s.RuleID == "edit-without-context" {
					found = true
					if s.Evidence != tc.count {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.count)
					}
					if !strings.Contains(s.Message, "get_context") {
						t.Errorf("Message = %q, want it to mention get_context", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("edit-without-context fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_SecretGuard(t *testing.T) {
	cases := []struct {
		name  string
		count int
		fires bool
	}{
		{"below threshold", evidenceThreshold - 1, false},
		{"at threshold", evidenceThreshold, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Inputs{Insights: stats.InsightsSummary{Violations: map[string]int{"secret-guard": tc.count}}}
			got := Evaluate(in)
			found := false
			for _, s := range got {
				if s.RuleID == "secret-guard" {
					found = true
					if s.Evidence != tc.count {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.count)
					}
					if !strings.Contains(s.Message, "secret-guard") {
						t.Errorf("Message = %q, want it to mention secret-guard", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("secret-guard fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_MemSearchZero(t *testing.T) {
	cases := []struct {
		name  string
		calls int
		zero  int
		fires bool
	}{
		{"below call floor even at 100% zero", 4, 4, false},
		{"at call floor, zero rate exactly half", 5, 3, true}, // 3/5 = 0.6 >= 0.5
		{"at call floor, zero rate below half", 5, 2, false},  // 2/5 = 0.4 < 0.5
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Inputs{Insights: stats.InsightsSummary{Tools: map[string]*stats.ToolInsight{
				"mem_search": {Calls: tc.calls, ZeroResults: tc.zero},
			}}}
			got := Evaluate(in)
			found := false
			for _, s := range got {
				if s.RuleID == "mem-search-zero" {
					found = true
					if s.Evidence != tc.zero {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.zero)
					}
					if !strings.Contains(s.Message, "mem_save") {
						t.Errorf("Message = %q, want it to mention mem_save", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("mem-search-zero fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_NoToolInsight_DoesNotFire(t *testing.T) {
	in := Inputs{Insights: stats.InsightsSummary{Tools: map[string]*stats.ToolInsight{}}}
	got := Evaluate(in)
	for _, s := range got {
		if s.RuleID == "mem-search-zero" {
			t.Errorf("mem-search-zero should not fire without a mem_search tool insight")
		}
	}
}

func TestEvaluate_Ordering(t *testing.T) {
	in := Inputs{
		CrystallizeNeeds: 2,
		Insights: stats.InsightsSummary{
			Violations: map[string]int{
				"edit-without-context": 10,
				"secret-guard":         10,
			},
			Tools: map[string]*stats.ToolInsight{
				"mem_search": {Calls: 5, ZeroResults: 5},
			},
		},
	}
	got := Evaluate(in)
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if prev.Evidence < cur.Evidence {
			t.Errorf("not sorted by Evidence desc: %+v before %+v", prev, cur)
		}
		if prev.Evidence == cur.Evidence && prev.RuleID > cur.RuleID {
			t.Errorf("tiebreak not by RuleID asc: %+v before %+v", prev, cur)
		}
	}
}

func TestEvaluate_EmptyInputsProducesNoSuggestions(t *testing.T) {
	got := Evaluate(Inputs{})
	if len(got) != 0 {
		t.Errorf("Evaluate(Inputs{}) = %+v, want empty", got)
	}
}
