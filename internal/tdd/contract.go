// Package tdd implements the tu-agent TDD dev-flow orchestrator: a
// deterministic state machine that sequences clean-context stage agents and
// routes on the structured Contract each one returns.
package tdd

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Contract status values returned by a stage.
const (
	StatusPass       = "pass"
	StatusRevise     = "revise"
	StatusFail       = "fail"
	StatusNeedsInput = "needs_input"
	StatusBlocked    = "blocked"
)

// Complexity classifications emitted by the architect.
const (
	ComplexityTrivial  = "trivial"
	ComplexityStandard = "standard"
	ComplexityComplex  = "complex"
)

// Artifact is an on-disk output of a stage (the payload lives in the file).
type Artifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Risk is a flagged concern with a severity.
type Risk struct {
	Severity string `json:"severity"`
	Desc     string `json:"desc"`
}

// Verdict is the judge/hardener gate result.
type Verdict struct {
	Result   string `json:"result"` // pass | revise | fail
	Feedback string `json:"feedback"`
	Score    int    `json:"score"`
}

// FeaturePlan is one feature the architect hands off for TDD: its slug and the
// scenario tags signed for it.
type FeaturePlan struct {
	Name      string   `json:"name"`
	Scenarios []string `json:"scenarios"`
	Kind      string   `json:"kind,omitempty"` // "" = normal TDD feature; "refactor" = no RED, keep suite green, not TDD-credited
}

// Contract is the routing envelope a stage returns. The orchestrator routes on
// Status (and Complexity for the architect); payloads live in Artifacts paths.
type Contract struct {
	Stage       string        `json:"stage"`
	Status      string        `json:"status"`
	Complexity  string        `json:"complexity,omitempty"`
	Artifacts   []Artifact    `json:"artifacts,omitempty"`
	Scenarios   []string      `json:"scenarios,omitempty"`
	Risks       []Risk        `json:"risks,omitempty"`
	Assumptions []string      `json:"assumptions,omitempty"`
	Handoff     string        `json:"handoff,omitempty"`
	Features    []FeaturePlan `json:"features,omitempty"`
	Verdict     *Verdict      `json:"verdict,omitempty"`
}

// ParseContract extracts the last ```json fenced block from text and unmarshals
// it. A missing block, malformed JSON, or empty status is an error — never a panic.
func ParseContract(text string) (Contract, error) {
	block, ok := lastJSONBlock(text)
	if !ok {
		return Contract{}, fmt.Errorf("tdd.ParseContract: no ```json contract block found")
	}
	var c Contract
	if err := json.Unmarshal([]byte(block), &c); err != nil {
		return Contract{}, fmt.Errorf("tdd.ParseContract: %w", err)
	}
	if c.Status == "" {
		return Contract{}, fmt.Errorf("tdd.ParseContract: contract missing status")
	}
	return c, nil
}

// planFeatures returns the architect's feature list, synthesizing a single plan
// from the legacy Handoff+Scenarios when no explicit list was emitted.
func planFeatures(c Contract) []FeaturePlan {
	if len(c.Features) > 0 {
		return c.Features
	}
	if c.Handoff != "" {
		return []FeaturePlan{{Name: c.Handoff, Scenarios: c.Scenarios}}
	}
	return nil
}

// lastJSONBlock returns the content of the last ```json ... ``` fence in text.
func lastJSONBlock(text string) (string, bool) {
	const open = "```json"
	const fence = "```"
	var last string
	found := false
	rest := text
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			break
		}
		after := rest[i+len(open):]
		j := strings.Index(after, fence)
		if j < 0 {
			break
		}
		last = strings.TrimSpace(after[:j])
		found = true
		rest = after[j+len(fence):]
	}
	return last, found
}
