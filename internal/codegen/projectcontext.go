package codegen

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

// SkillDoc is one generated SKILL.md fed into project-context synthesis.
type SkillDoc struct {
	Name    string
	Content string
}

const (
	projCtxReservedOut  = 2048 // tokens reserved for model output
	projCtxSystemTokens = 400  // approximate system prompt size
)

// BuildProjectContextPrompt is the system prompt for CLAUDE.md enrichment.
func BuildProjectContextPrompt() string {
	return `You are documenting a codebase for an AI assistant's CLAUDE.md.
From the per-domain skill notes provided, write exactly two concise sections:

## Coding Conventions
- 5-10 bullets: the most important repo-wide conventions (naming, error handling, recurring patterns).

## Key Entry Points
- 3-5 entries, each "path — why it matters": the files most central to understanding the system.

Rules:
- Be concise. CLAUDE.md is loaded at every session start.
- Use ONLY what the skill notes support. Invent nothing.
- Output ONLY the two sections, starting with "## Coding Conventions". No preamble, no code fences.`
}

// BuildProjectContextMessage builds the user message from the generated skills,
// trimmed to maxBytes (dropping skills from the end). maxBytes <= 0 means no limit.
func BuildProjectContextMessage(project string, skills []SkillDoc, maxBytes int) string {
	ds := append([]SkillDoc(nil), skills...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })

	build := func(subset []SkillDoc) string {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Project: %s\n\nDomain skill notes:\n", project)
		for _, s := range subset {
			fmt.Fprintf(&sb, "\n=== %s ===\n%s\n", s.Name, s.Content)
		}
		sb.WriteString("\nWrite the two sections now.")
		return sb.String()
	}
	if maxBytes <= 0 {
		return build(ds)
	}
	for len(ds) > 1 {
		msg := build(ds)
		if len(msg) <= maxBytes {
			return msg
		}
		ds = ds[:len(ds)-1]
	}
	return build(ds)
}

// GenerateProjectContext runs one tool-less turn to produce the Coding
// Conventions and Key Entry Points sections for CLAUDE.md. contextSize is the
// effective window (0 = no limit). Returns an error (not junk) when the output
// is empty or missing the required sections, so the caller can keep the init
// placeholder intact.
func GenerateProjectContext(ctx context.Context, project string, skills []SkillDoc, prov provider.Provider, tel *telemetry.Logger, contextSize int) (string, error) {
	maxBytes := 0
	if contextSize > 0 {
		available := contextSize - projCtxReservedOut - projCtxSystemTokens
		if available > 0 {
			maxBytes = available * 4
		}
	}
	tools := tool.NewRegistry()
	orch := orchestrator.New(prov, tools, tel, BuildProjectContextPrompt(), "learn-claude-md")
	raw, err := orch.Chat(ctx, BuildProjectContextMessage(project, skills, maxBytes))
	if err != nil {
		return "", fmt.Errorf("codegen.GenerateProjectContext: %w", err)
	}
	content := stripCodeFence(raw)
	if content == "" || !strings.Contains(content, "## Coding Conventions") {
		return "", fmt.Errorf("codegen.GenerateProjectContext: output missing required sections")
	}
	return content, nil
}
