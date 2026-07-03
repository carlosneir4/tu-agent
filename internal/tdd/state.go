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
}

// BeginRun builds a fresh State with every feature pending.
func BeginRun(task, branch string, features []FeaturePlan) State {
	fs := make([]FeatureState, 0, len(features))
	for _, f := range features {
		fs = append(fs, FeatureState{Name: f.Name, Status: "pending", Kind: f.Kind})
	}
	return State{Version: StateVersion, Task: task, Branch: branch, Features: fs}
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

// Resumable reports whether the run has at least one pending feature to continue.
func (s State) Resumable() bool {
	_, ok := s.NextPending()
	return ok
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
	return s, nil
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
