package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// FingerprintKeyFiles returns a stable hash over the contents of keyFiles
// (resolved against root). Order-independent. Missing files contribute a
// "<missing>" marker so adding/removing a file changes the fingerprint.
func FingerprintKeyFiles(root string, keyFiles []string) (string, error) {
	sorted := make([]string, len(keyFiles))
	copy(sorted, keyFiles)
	sort.Strings(sorted)
	h := sha256.New()
	for _, rel := range sorted {
		fmt.Fprintf(h, "%s\n", rel)
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if os.IsNotExist(err) {
				h.Write([]byte("<missing>\n"))
				continue
			}
			return "", fmt.Errorf("codegen.FingerprintKeyFiles: %w", err)
		}
		h.Write(data)
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SkillFingerprints maps skill name -> Key Files fingerprint.
type SkillFingerprints map[string]string

// LoadFingerprints reads a fingerprints JSON file. A missing file returns an
// empty map (not an error).
func LoadFingerprints(path string) (SkillFingerprints, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SkillFingerprints{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("codegen.LoadFingerprints: %w", err)
	}
	var fp SkillFingerprints
	if err := json.Unmarshal(data, &fp); err != nil {
		return nil, fmt.Errorf("codegen.LoadFingerprints: %w", err)
	}
	if fp == nil {
		fp = SkillFingerprints{}
	}
	return fp, nil
}

// WriteJSON writes fingerprints as pretty JSON.
func (fp SkillFingerprints) WriteJSON(path string) error {
	data, err := json.MarshalIndent(fp, "", "  ")
	if err != nil {
		return fmt.Errorf("codegen.SkillFingerprints.WriteJSON: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("codegen.SkillFingerprints.WriteJSON: %w", err)
	}
	return nil
}

// RecordFingerprints computes current fingerprints for every non-architecture
// skill, suitable for WriteJSON.
func RecordFingerprints(root string, skills []Skill) (SkillFingerprints, error) {
	fp := SkillFingerprints{}
	for _, s := range skills {
		if s.Name == "architecture" {
			continue
		}
		h, err := FingerprintKeyFiles(root, ParseKeyFiles(s.Body))
		if err != nil {
			return nil, err
		}
		fp[s.Name] = h
	}
	return fp, nil
}

// SkillState is the freshness of one skill relative to recorded fingerprints.
type SkillState struct {
	Name   string
	Status string // "up-to-date" | "stale" | "new"
}

// ComputeSkillStatus compares each skill's current Key Files fingerprint to the
// recorded one. No recorded fingerprint => "new"; changed => "stale"; else
// "up-to-date". The "architecture" skill is skipped (derived, not a domain).
func ComputeSkillStatus(root string, skills []Skill, recorded SkillFingerprints) ([]SkillState, error) {
	var states []SkillState
	for _, s := range skills {
		if s.Name == "architecture" {
			continue
		}
		cur, err := FingerprintKeyFiles(root, ParseKeyFiles(s.Body))
		if err != nil {
			return nil, err
		}
		status := "stale"
		if rec, ok := recorded[s.Name]; !ok {
			status = "new"
		} else if rec == cur {
			status = "up-to-date"
		}
		states = append(states, SkillState{Name: s.Name, Status: status})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, nil
}
