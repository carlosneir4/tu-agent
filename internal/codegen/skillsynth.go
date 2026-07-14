package codegen

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/carlosneir4/tu-agent/internal/orchestrator"
	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

// NoteInput is one memory note fed into skill synthesis. codegen keeps its own
// input type so it does not depend on internal/memory.
type NoteInput struct {
	Topic   string
	Type    string
	Content string
}

// BuildSkillSynthesisPrompt is the system prompt for crystallizing a cluster of
// related notes into one project skill.
func BuildSkillSynthesisPrompt() string {
	return strings.Join([]string{
		"You consolidate a cluster of related project memory notes into ONE reusable skill.",
		"Output a single SKILL.md document and nothing else.",
		"It MUST begin with YAML frontmatter containing `name:` and a `description:`",
		"that states WHEN to use the skill (so it auto-activates for that task area).",
		"Then write the consolidated standard: the conventions, patterns, gotchas, and",
		"concrete examples the notes establish — a standalone guide, not a list of links.",
		"Do NOT invent facts not supported by the notes. Do NOT include any HTML comment",
		"marker or provenance line; that is added separately.",
	}, "\n")
}

// trimToRuneBoundary truncates s to at most maxBytes bytes without splitting a
// multi-byte UTF-8 rune (maxBytes <= 0 means no limit). It backs off to the
// last rune start at or before maxBytes.
func trimToRuneBoundary(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// BuildSkillSynthesisMessage renders the cluster (label + member notes) into the
// user message, trimmed to maxBytes (0 = no limit) like the architecture synth.
func BuildSkillSynthesisMessage(label string, notes []NoteInput, maxBytes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Crystallize a skill named %q from these %d related notes:\n\n", label, len(notes))
	for _, n := range notes {
		fmt.Fprintf(&b, "## %s (%s)\n%s\n\n", n.Topic, n.Type, n.Content)
	}
	out := b.String()
	return trimToRuneBoundary(out, maxBytes)
}

// GenerateSkill runs one tool-less model turn and returns the SKILL.md body.
// The caller adds provenance and writes/stores the result.
func GenerateSkill(ctx context.Context, label string, notes []NoteInput, prov provider.Provider, tel *telemetry.Logger, contextSize int) (string, error) {
	maxBytes := 0
	if contextSize > 0 {
		available := contextSize - synthesisReservedOut - synthesisSystemTokens
		if available > 0 {
			maxBytes = available * 4
		}
	}
	tools := tool.NewRegistry()
	orch := orchestrator.New(prov, tools, tel, BuildSkillSynthesisPrompt(), "crystallize")
	content, err := orch.Chat(ctx, BuildSkillSynthesisMessage(label, notes, maxBytes))
	if err != nil {
		return "", fmt.Errorf("codegen.GenerateSkill: %w", err)
	}
	return content, nil
}
