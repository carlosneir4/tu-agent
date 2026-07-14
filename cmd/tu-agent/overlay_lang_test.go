package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

// readYAML unmarshals a YAML file into a map, failing the test on any error.
func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// @s4: SeedProjectLanguage persists the resolved language into
// .tu-agent/config.yaml when none is set, and reports that the file changed.
func TestSeedProjectLanguage_WritesWhenAbsent(t *testing.T) {
	root := t.TempDir()

	changed, err := tdd.SeedProjectLanguage(root, "java")
	if err != nil {
		t.Fatalf("SeedProjectLanguage: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true (first seed must report a change)")
	}

	m := readYAML(t, filepath.Join(root, ".tu-agent", "config.yaml"))
	tddSection, _ := m["tdd"].(map[string]any)
	if got := tddSection["language"]; got != "java" {
		t.Fatalf("tdd.language = %v, want java", got)
	}
}

// @s5: SeedProjectLanguage is idempotent and never clobbers a user-set value,
// leaving sibling keys in the tdd section intact.
func TestSeedProjectLanguage_IdempotentPreservesSibling(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".tu-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "tdd:\n  language: go\n  test_command: go test ./...\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := tdd.SeedProjectLanguage(root, "java")
	if err != nil {
		t.Fatalf("SeedProjectLanguage: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false (must not clobber a user value)")
	}

	m := readYAML(t, filepath.Join(dir, "config.yaml"))
	tddSection, _ := m["tdd"].(map[string]any)
	if got := tddSection["language"]; got != "go" {
		t.Fatalf("tdd.language = %v, want go (user value clobbered)", got)
	}
	if got := tddSection["test_command"]; got != "go test ./..." {
		t.Fatalf("tdd.test_command = %v, want go test ./... (sibling key dropped)", got)
	}
}
