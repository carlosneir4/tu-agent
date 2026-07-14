package config

import (
	"os"
	"path/filepath"
	"testing"
)

// @s1: tdd.language unmarshals from a project-layer config.yaml and survives the
// merge into the loaded Config. Fails now because mergeInto drops Tdd.Language.
func TestLoad_TddLanguage_ProjectLayer(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "config.yaml"),
		[]byte("tdd:\n  language: java\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tdd.Language != "java" {
		t.Fatalf("Tdd.Language = %q, want %q (project-layer language dropped by merge)", cfg.Tdd.Language, "java")
	}
}

// @s2: a non-empty project-layer Tdd.Language wins over an empty base value.
func TestMergeIntoTddLanguage_NonEmptyWins(t *testing.T) {
	base := Config{}
	proj := Config{Tdd: TddConfig{Language: "python"}}
	mergeInto(&base, proj)
	if base.Tdd.Language != "python" {
		t.Fatalf("Tdd.Language = %q, want %q (mergeInto dropped the new field)", base.Tdd.Language, "python")
	}
}

// @s3: an empty project-layer Tdd.Language must not clobber an existing value.
func TestMergeIntoTddLanguage_EmptyDoesNotClobber(t *testing.T) {
	base := Config{Tdd: TddConfig{Language: "go"}}
	proj := Config{Tdd: TddConfig{Language: ""}}
	mergeInto(&base, proj)
	if base.Tdd.Language != "go" {
		t.Fatalf("Tdd.Language = %q, want %q (empty layer clobbered the value)", base.Tdd.Language, "go")
	}
}
