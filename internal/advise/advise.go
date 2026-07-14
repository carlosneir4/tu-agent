// Package advise evaluates deterministic guardrail rules against telemetry
// insights and crystallize state, producing terse, evidence-bearing
// suggestions ("nudges"). It is pure: no memory-store or filesystem access —
// the cmd layer gathers Inputs and does all I/O.
package advise

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

// evidenceThreshold is the minimum count for behavioral rules (rules driven
// by a violation/tool counter, as opposed to crystallize-ready's own
// needs>=1 semantics) to fire.
const evidenceThreshold = 3

// memSearchMinCalls is the minimum mem_search call volume before the
// mem-search-zero rule judges its zero-result rate meaningful.
const memSearchMinCalls = 5

// memSearchZeroRateFloor is the minimum zero-result rate (zero/calls) for
// the mem-search-zero rule to fire, once memSearchMinCalls is met.
const memSearchZeroRateFloor = 0.5

// Suggestion is one actionable, evidence-bearing nudge produced by a rule.
type Suggestion struct {
	// RuleID is a stable identifier: "crystallize-ready" |
	// "edit-without-context" | "secret-guard" | "mem-search-zero".
	RuleID string
	// Message is a one-line, evidence-bearing, imperative suggestion.
	Message string
	// Evidence is the count/needs behind the suggestion, used for dedup
	// (evidence-growth gating) and ordering.
	Evidence int
}

// Inputs gathers everything Evaluate's rules read. The cmd layer builds this
// from telemetry (internal/stats) and crystallize.Detect/Classify.
type Inputs struct {
	// Insights is the aggregated telemetry measurement report.
	Insights stats.InsightsSummary
	// CrystallizeNeeds is the count of clusters whose skill status is not
	// current (crystallize.StatusCurrent).
	CrystallizeNeeds int
}

// Evaluate runs all rules and returns every suggestion meeting its
// threshold, deterministically ordered (Evidence desc, then RuleID asc).
// Pure: it holds no state itself — see Filter for dedup/dismiss/budget.
func Evaluate(in Inputs) []Suggestion {
	var out []Suggestion

	if needs := in.CrystallizeNeeds; needs >= 1 {
		out = append(out, Suggestion{
			RuleID:   "crystallize-ready",
			Evidence: needs,
			Message:  fmt.Sprintf("%d note cluster(s) ready to crystallize — run `tu-agent memory crystallize`", needs),
		})
	}

	if n := in.Insights.Violations["edit-without-context"]; n >= evidenceThreshold {
		out = append(out, Suggestion{
			RuleID:   "edit-without-context",
			Evidence: n,
			Message:  fmt.Sprintf("edited without a graph context query %d times — run get_context before editing", n),
		})
	}

	if n := in.Insights.Violations["secret-guard"]; n >= evidenceThreshold {
		out = append(out, Suggestion{
			RuleID:   "secret-guard",
			Evidence: n,
			Message:  fmt.Sprintf("the secret-guard blocked %d attempt(s) to read/modify secret files", n),
		})
	}

	if ti, ok := in.Insights.Tools["mem_search"]; ok {
		calls, zero := ti.Calls, ti.ZeroResults
		if calls >= memSearchMinCalls && float64(zero)/float64(calls) >= memSearchZeroRateFloor {
			out = append(out, Suggestion{
				RuleID:   "mem-search-zero",
				Evidence: zero,
				Message: fmt.Sprintf("memory search returned nothing %.0f%% of the time (%d/%d) — capture decisions with mem_save",
					100*float64(zero)/float64(calls), zero, calls),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Evidence != out[j].Evidence {
			return out[i].Evidence > out[j].Evidence
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

// RuleState tracks one rule's dedup/dismiss state across advise --nudge runs.
type RuleState struct {
	// LastShownEvidence is the Evidence value at which this rule was last
	// shown; a suggestion re-fires only once its Evidence grows past this.
	LastShownEvidence int `json:"last_shown_evidence"`
	// Dismissed permanently suppresses this rule (advise dismiss <rule-id>).
	Dismissed bool `json:"dismissed,omitempty"`
}

// State is the persisted advise state (one RuleState per rule ID), stored in
// the memory store's metadata table by the cmd layer.
type State struct {
	Rules map[string]RuleState `json:"rules"`
}

// ParseState parses a persisted state string. An empty string is a valid
// "no state yet" representation and returns an empty State; a non-empty but
// malformed string is an error.
func ParseState(raw string) (State, error) {
	if raw == "" {
		return State{Rules: map[string]RuleState{}}, nil
	}
	var st State
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return State{}, fmt.Errorf("advise.ParseState: %w", err)
	}
	if st.Rules == nil {
		st.Rules = map[string]RuleState{}
	}
	return st, nil
}

// Marshal serializes State for persistence.
func (s State) Marshal() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("advise.State.Marshal: %w", err)
	}
	return string(b), nil
}

// Filter applies the dedup/dismiss guardrails and a budget for the nudge
// channel. A suggestion is shown iff: it is not dismissed AND its Evidence
// is greater than the rule's LastShownEvidence (evidence-growth is the
// dedup + rate-limit — a rule is not re-nudged until something changes).
// Filter returns the suggestions to show (capped at budget, keeping the
// input order) and the new State to persist: LastShownEvidence is bumped to
// match Evidence for every shown rule, and left untouched for every other
// rule (including ones eligible but dropped by the budget cap, so they
// remain eligible next run).
func Filter(suggestions []Suggestion, st State, budget int) (show []Suggestion, next State) {
	next = State{Rules: make(map[string]RuleState, len(st.Rules))}
	for id, rs := range st.Rules {
		next.Rules[id] = rs
	}
	for _, sg := range suggestions {
		if len(show) >= budget {
			break
		}
		rs := next.Rules[sg.RuleID]
		if rs.Dismissed || sg.Evidence <= rs.LastShownEvidence {
			continue
		}
		show = append(show, sg)
		rs.LastShownEvidence = sg.Evidence
		next.Rules[sg.RuleID] = rs
	}
	return show, next
}
