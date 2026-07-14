package tdd

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestSeedProjectTestCommand(t *testing.T) {
	root := t.TempDir()

	// Empty testCmd is a no-op.
	changed, err := SeedProjectTestCommand(root, "")
	if err != nil || changed {
		t.Fatalf("empty testCmd: changed=%v err=%v, want false,nil", changed, err)
	}

	// First seed writes the command.
	changed, err = SeedProjectTestCommand(root, "./gradlew test")
	if err != nil || !changed {
		t.Fatalf("first seed: changed=%v err=%v, want true,nil", changed, err)
	}
	cfgPath := filepath.Join(root, ".tu-agent", "config.yaml")
	m := readYAML(t, cfgPath)
	tddSection, _ := m["tdd"].(map[string]any)
	if got := tddSection["test_command"]; got != "./gradlew test" {
		t.Fatalf("test_command = %v, want ./gradlew test", got)
	}

	// Second seed is idempotent: does not overwrite an existing command.
	changed, err = SeedProjectTestCommand(root, "mvn test")
	if err != nil || changed {
		t.Fatalf("idempotent seed: changed=%v err=%v, want false,nil", changed, err)
	}
	m = readYAML(t, cfgPath)
	tddSection, _ = m["tdd"].(map[string]any)
	if got := tddSection["test_command"]; got != "./gradlew test" {
		t.Fatalf("test_command overwritten to %v, want ./gradlew test", got)
	}
}

func TestSeedPreservesOtherKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".tu-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "routing:\n  default: local\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SeedProjectTestCommand(root, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	m := readYAML(t, filepath.Join(dir, "config.yaml"))
	if _, ok := m["routing"]; !ok {
		t.Fatalf("routing key dropped: %v", m)
	}
}
