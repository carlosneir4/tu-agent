package tdd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// StateVersion is the on-disk schema version of the run state.
const StateVersion = 1

// ScenarioState tracks one @s scenario's progress within a feature's sandwich.
type ScenarioState struct {
	Tag   string `json:"tag"`
	Phase string `json:"phase"` // red | green | done
	Kind  string `json:"kind"`  // tdd | regression | refactor
}

// FeatureState tracks one feature's terminal status within a run.
type FeatureState struct {
	Name      string          `json:"name"`
	Status    string          `json:"status"` // pending | pass | blocked
	Scenarios []ScenarioState `json:"scenarios,omitempty"`
	Kind      string          `json:"kind,omitempty"` // "" = normal TDD feature; "refactor" = no RED, not TDD-credited
}

// State is the persisted state of one (possibly multi-feature) run.
type State struct {
	Version  int            `json:"version"`
	Task     string         `json:"task"`
	Branch   string         `json:"branch,omitempty"`
	Features []FeatureState `json:"features"`
	// Review tracks the post-features design review gate: "" (legacy, no
	// review) | "pending" | "pass" | "skipped". Empty on files written by
	// older binaries so they never gain review semantics retroactively.
	Review string `json:"review,omitempty"`
}

// BeginRun builds a fresh State with every feature pending. It rejects
// duplicate feature names as a second line of defense against a caller that
// skipped planFeatures' dedup.
func BeginRun(task, branch string, features []FeaturePlan) (State, error) {
	seen := make(map[string]bool, len(features))
	fs := make([]FeatureState, 0, len(features))
	for _, f := range features {
		if seen[f.Name] {
			return State{}, fmt.Errorf("tdd.BeginRun: duplicate feature %q", f.Name)
		}
		seen[f.Name] = true
		fs = append(fs, FeatureState{Name: f.Name, Status: "pending", Kind: f.Kind})
	}
	return State{Version: StateVersion, Task: task, Branch: branch, Features: fs}, nil
}

// Feature returns the FeatureState for name, and whether it was found.
func (s State) Feature(name string) (FeatureState, bool) {
	for _, f := range s.Features {
		if f.Name == name {
			return f, true
		}
	}
	return FeatureState{}, false
}

// NextPending returns the first pending feature name.
func (s State) NextPending() (string, bool) {
	for _, f := range s.Features {
		if f.Status == "pending" {
			return f.Name, true
		}
	}
	return "", false
}

// Mark sets the status of the named feature (no-op if unknown).
func (s *State) Mark(name, status string) {
	for i := range s.Features {
		if s.Features[i].Name == name {
			s.Features[i].Status = status
			return
		}
	}
}

// SetScenario upserts a scenario's state on the named feature (by tag).
func (s *State) SetScenario(feature string, sc ScenarioState) {
	for i := range s.Features {
		if s.Features[i].Name != feature {
			continue
		}
		for j := range s.Features[i].Scenarios {
			if s.Features[i].Scenarios[j].Tag == sc.Tag {
				s.Features[i].Scenarios[j] = sc
				return
			}
		}
		s.Features[i].Scenarios = append(s.Features[i].Scenarios, sc)
		return
	}
}

// Resumable reports whether the run has more work to continue: either a
// pending feature remains, or every feature has passed and the design review
// gate is still pending. A fully-passed run with an empty review (legacy),
// "pass", or "skipped" review is done, not resumable.
func (s State) Resumable() bool {
	if _, ok := s.NextPending(); ok {
		return true
	}
	pass, pending, blocked := s.Summary()
	return pending == 0 && blocked == 0 && pass > 0 && s.Review == "pending"
}

// Summary counts features by terminal status.
func (s State) Summary() (pass, pending, blocked int) {
	for _, f := range s.Features {
		switch f.Status {
		case "pass":
			pass++
		case "blocked":
			blocked++
		default:
			pending++
		}
	}
	return
}

// LoadState reads state from path. A missing file is a zero State, not an error.
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("tdd.LoadState: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("tdd.LoadState: %w", err)
	}
	s.Features = dedupeFeatures(s.Features)
	return s, nil
}

// dedupeFeatures drops later features that repeat an earlier one's Name,
// keeping the first occurrence. It defends the resume path: BeginRun's
// duplicate guard only covers construction, so a state.json written by an
// older binary (or hand-edited) can still reach disk with duplicate names.
// Without this, NextPending/Mark loop forever over the same stuck name
// instead of erroring or brick-ing an existing run — so this is silent and
// non-erroring by design.
func dedupeFeatures(features []FeatureState) []FeatureState {
	seen := make(map[string]bool, len(features))
	out := make([]FeatureState, 0, len(features))
	for _, f := range features {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f)
	}
	return out
}

// SaveState writes state to path atomically (temp file + rename), creating
// parent directories as needed.
func SaveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("tdd.SaveState: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("tdd.SaveState: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("tdd.SaveState: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("tdd.SaveState: %w", err)
	}
	return nil
}
