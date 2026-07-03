package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// seedProjectTestCommand persists testCmd as tdd.test_command in
// <root>/.tu-agent/config.yaml, merging into any existing YAML so other keys
// survive. It is idempotent: a no-op when testCmd is empty or a non-empty
// tdd.test_command is already set. Returns whether the file was changed.
func seedProjectTestCommand(root, testCmd string) (bool, error) {
	testCmd = strings.TrimSpace(testCmd)
	if testCmd == "" {
		return false, nil
	}
	path := filepath.Join(root, ".tu-agent", "config.yaml")
	m := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return false, fmt.Errorf("seedProjectTestCommand: parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("seedProjectTestCommand: read %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	tddSection, _ := m["tdd"].(map[string]any)
	if tddSection == nil {
		tddSection = map[string]any{}
	}
	if existing, _ := tddSection["test_command"].(string); strings.TrimSpace(existing) != "" {
		return false, nil // never clobber a user-set command
	}
	tddSection["test_command"] = testCmd
	m["tdd"] = tddSection

	out, err := yaml.Marshal(m)
	if err != nil {
		return false, fmt.Errorf("seedProjectTestCommand: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("seedProjectTestCommand: mkdir: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, fmt.Errorf("seedProjectTestCommand: write: %w", err)
	}
	return true, nil
}
