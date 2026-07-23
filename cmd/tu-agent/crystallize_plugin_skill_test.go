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

// TestCrystallizePluginSkillWidensNet pins the "widen the net" step: crystallize
// must look past the cluster for related notes that community detection mapped
// elsewhere, using mem_related, before synthesizing.
func TestCrystallizePluginSkillWidensNet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "crystallize", "SKILL.md"))
	if err != nil {
		t.Fatalf("read crystallize SKILL.md: %v", err)
	}
	s := string(raw)
	for _, want := range []string{"Widen the net", "mem_related", "clustered elsewhere"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md missing widen-the-net anchor %q", want)
		}
	}
}

// TestCrystallizePluginSkillGeneratesTerse pins the terse-output directive in the
// generate step: the produced skill must be a checklist/table with oracles, not a
// prose walkthrough.
func TestCrystallizePluginSkillGeneratesTerse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "crystallize", "SKILL.md"))
	if err != nil {
		t.Fatalf("read crystallize SKILL.md: %v", err)
	}
	s := string(raw)
	for _, want := range []string{"terse, imperative", "oracle", "Cut whys"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md missing terse-output anchor %q", want)
		}
	}
}
