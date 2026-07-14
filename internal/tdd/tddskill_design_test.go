package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// @s3 — the plugin conductor's Step 1 (analyst) mentions the design exploration:
// proposing 2-3 approaches with trade-offs when more than one viable approach
// exists, and recording the decision in the spec's "## Design" section.
func TestTddSkillStep1MentionsDesignExploration(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "tdd", "SKILL.md"))
	if err != nil {
		t.Fatalf("read tdd SKILL.md: %v", err)
	}
	s := string(raw)

	// The design-exploration guidance must live within Step 1 (analyst), before Step 2.
	step1 := s
	if i := strings.Index(s, "## Step 1"); i >= 0 {
		step1 = s[i:]
	}
	if j := strings.Index(step1, "## Step 2"); j >= 0 {
		step1 = step1[:j]
	}

	for _, want := range []string{
		"2-3 approaches",
		"trade-off",
		"viable approach",
		"## Design",
	} {
		if !strings.Contains(step1, want) {
			t.Errorf("tdd SKILL.md Step 1 must mention the design exploration; missing %q", want)
		}
	}
}
