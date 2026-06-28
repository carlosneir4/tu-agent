package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroundworkSkillExistsAndIsWellFormed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "groundwork", "SKILL.md"))
	if err != nil {
		t.Fatalf("read groundwork SKILL.md: %v", err)
	}
	s := string(raw)

	// Frontmatter declares the skill name the knowledge-block directive references.
	if !strings.Contains(s, "name: groundwork") {
		t.Errorf("SKILL.md missing `name: groundwork` frontmatter")
	}
	// Description triggers on build/implementation work.
	for _, want := range []string{"description:", "build", "implement"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md description missing %q", want)
		}
	}
	// The five-phase posture, anchored in graph + memory, with capture and tdd handoff.
	for _, want := range []string{
		"Anchor first",
		"get_context",
		"mem_search",
		"mem_save",
		"`tdd`",
		"just do it", // escape hatch
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md body missing %q", want)
		}
	}
}
