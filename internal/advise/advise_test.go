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

func TestEvaluate_LearnStale(t *testing.T) {
	cases := []struct {
		name      string
		uncovered int
		fires     bool
	}{
		{"zero does not fire", 0, false},
		{"below threshold does not fire", learnStaleThreshold - 1, false},
		{"at threshold fires", learnStaleThreshold, true},
		{"well over threshold fires", learnStaleThreshold * 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(Inputs{UncoveredFiles: tc.uncovered})
			found := false
			for _, s := range got {
				if s.RuleID == "learn-stale" {
					found = true
					if s.Evidence != tc.uncovered {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.uncovered)
					}
					if !strings.Contains(s.Message, "/tu-agent:learn") {
						t.Errorf("Message = %q, want it to mention /tu-agent:learn", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("learn-stale fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_SkillPending(t *testing.T) {
	cases := []struct {
		name    string
		pending int
		fires   bool
	}{
		{"zero does not fire", 0, false},
		{"one fires", 1, true},
		{"several fires", 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(Inputs{PendingForeignSkills: tc.pending})
			found := false
			for _, s := range got {
				if s.RuleID == "skill-pending" {
					found = true
					if s.Evidence != tc.pending {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.pending)
					}
					if !strings.Contains(s.Message, "tu-agent memory pending") {
						t.Errorf("Message = %q, want it to mention `tu-agent memory pending`", s.Message)
					}
					if !strings.Contains(s.Message, "tu-agent memory approve-skill") {
						t.Errorf("Message = %q, want it to mention `tu-agent memory approve-skill`", s.Message)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("skill-pending fired = %v, want %v", found, tc.fires)
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

func TestEvaluate_GateFriction(t *testing.T) {
	cases := []struct {
		name     string
		failures map[string]int
		fires    bool
		reason   string
		evidence int
		wantTip  string
	}{
		{"empty does not fire", map[string]int{}, false, "", 0, ""},
		{"below threshold does not fire", map[string]int{"build_failed": evidenceThreshold - 1}, false, "", 0, ""},
		{"at threshold fires", map[string]int{"build_failed": evidenceThreshold}, true, "build_failed", evidenceThreshold, "tdd.build_tags"},
		{"mixed reasons individually below threshold does not fire", map[string]int{"build_failed": 2, "test_failed": 2}, false, "", 0, ""},
		{"dominant reason picked over lesser reason", map[string]int{"build_failed": evidenceThreshold, "test_failed": 1}, true, "build_failed", evidenceThreshold, "tdd.build_tags"},
		{"tie breaks lexicographically first reason", map[string]int{"test_failed": evidenceThreshold, "build_failed": evidenceThreshold}, true, "build_failed", evidenceThreshold, "tdd.build_tags"},
		{"non-build_failed reason gets the generic tip", map[string]int{"test_failed": evidenceThreshold}, true, "test_failed", evidenceThreshold, "tu-agent stats --flow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Inputs{Insights: stats.InsightsSummary{GateFailures: tc.failures}}
			got := Evaluate(in)
			found := false
			for _, s := range got {
				if s.RuleID == "gate-friction" {
					found = true
					if s.Evidence != tc.evidence {
						t.Errorf("Evidence = %d, want %d", s.Evidence, tc.evidence)
					}
					if !strings.Contains(s.Message, tc.reason) {
						t.Errorf("Message = %q, want it to mention reason %q", s.Message, tc.reason)
					}
					if !strings.Contains(s.Message, tc.wantTip) {
						t.Errorf("Message = %q, want it to mention tip %q", s.Message, tc.wantTip)
					}
				}
			}
			if found != tc.fires {
				t.Errorf("gate-friction fired = %v, want %v", found, tc.fires)
			}
		})
	}
}

func TestEvaluate_EmptyInputsProducesNoSuggestions(t *testing.T) {
	got := Evaluate(Inputs{})
	if len(got) != 0 {
		t.Errorf("Evaluate(Inputs{}) = %+v, want empty", got)
	}
}
