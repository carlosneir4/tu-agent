package tdd

import (
	"strings"
	"testing"
)

// @s4(b) — ComposeStagePrompt resolves a non-empty body for every dispatched
// role on a repo with no materialized agents. This works via the F7-B embedded
// generic shell fallback: every role resolves without a .claude/agents/<role>.md.
func TestComposeStagePromptFallbackAllStages(t *testing.T) {
	// No .claude/agents at all — every role must resolve via the embedded shell.
	root := t.TempDir()
	stages := []string{
		"analyst", "architect", "craftsman", "judge", "scribe",
		"review", "review-fixer", "test-writer", "implementer", "refactor",
	}
	for _, stage := range stages {
		out, err := ComposeStagePrompt(root, stage, ".tu-agent/tdd/x")
		if err != nil {
			t.Fatalf("composeStagePrompt(%q) must resolve via fallback, got error: %v", stage, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("composeStagePrompt(%q) returned an empty prompt", stage)
		}
	}
}
