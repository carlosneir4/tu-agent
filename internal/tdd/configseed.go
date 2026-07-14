package tdd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SeedTddStringField persists val as tdd.<key> in <root>/.tu-agent/config.yaml,
// merging into any existing YAML so other keys survive. It is idempotent: a
// no-op when val is empty or a non-empty tdd.<key> is already set (a user value
// is never clobbered). Returns whether the file was changed.
func SeedTddStringField(root, key, val string) (bool, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return false, nil
	}
	path := filepath.Join(root, ".tu-agent", "config.yaml")
	m := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return false, fmt.Errorf("seedTddStringField(%s): parse %s: %w", key, path, err)
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("seedTddStringField(%s): read %s: %w", key, path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	tddSection, _ := m["tdd"].(map[string]any)
	if tddSection == nil {
		tddSection = map[string]any{}
	}
	if existing, _ := tddSection[key].(string); strings.TrimSpace(existing) != "" {
		return false, nil // never clobber a user-set value
	}
	tddSection[key] = val
	m["tdd"] = tddSection

	out, err := yaml.Marshal(m)
	if err != nil {
		return false, fmt.Errorf("seedTddStringField(%s): marshal: %w", key, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("seedTddStringField(%s): mkdir: %w", key, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, fmt.Errorf("seedTddStringField(%s): write: %w", key, err)
	}
	return true, nil
}

// SeedProjectTestCommand persists testCmd as tdd.test_command in the project
// config (see SeedTddStringField).
func SeedProjectTestCommand(root, testCmd string) (bool, error) {
	return SeedTddStringField(root, "test_command", testCmd)
}

// SeedProjectLanguage persists lang as tdd.language in the project config so the
// runtime overlay language is deterministic (see SeedTddStringField).
func SeedProjectLanguage(root, lang string) (bool, error) {
	return SeedTddStringField(root, "language", lang)
}
