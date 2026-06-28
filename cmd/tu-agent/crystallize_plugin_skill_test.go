package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrystallizePluginSkillWellFormed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "crystallize", "SKILL.md"))
	if err != nil {
		t.Fatalf("read crystallize SKILL.md: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "name: crystallize") {
		t.Error("missing `name: crystallize` frontmatter")
	}
	for _, want := range []string{"description:", "mem_clusters", "conflicts_with", "crystallize_save"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
}
