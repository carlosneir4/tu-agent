package codegen

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

// ErrMergedParseFailed signals the merged response could not be split into both
// sections, so the caller should fall back to the two separate generators.
var ErrMergedParseFailed = errors.New("codegen: merged arch+context response unparseable")

const (
	archCtxMarkerArch   = "=== ARCHITECTURE ==="
	archCtxMarkerCtx    = "=== PROJECT-CONTEXT ==="
	archCtxReservedOut  = 4096
	archCtxSystemTokens = 600
)

// BuildArchContextPrompt is the system prompt for the merged generation.
func BuildArchContextPrompt() string {
	return `You document a codebase for an AI assistant. Output TWO sections,
each introduced by its exact marker line and nothing before the first marker:

=== ARCHITECTURE ===
A concise architecture overview skill: the domain map, how domains depend on
each other, and where the main flows live. Use only the facts provided.

=== PROJECT-CONTEXT ===
## Coding Conventions
- 5-10 bullets: the most important repo-wide conventions.
## Key Entry Points
- 3-5 entries, each "path — why it matters".

Rules: be concise; invent nothing; output the two marker lines exactly; no code fences.`
}

// BuildArchContextMessage combines the architecture facts (domains + edges) and
// the per-domain skill notes into one user message, trimmed to maxBytes by
// dropping skill notes from the end. maxBytes <= 0 means no limit.
func BuildArchContextMessage(project string, domains []DomainFact, domainEdges []Edge, skills []SkillDoc, maxBytes int) string {
	head := BuildSynthesisMessage(project, domains, domainEdges, 0)
	build := func(subset []SkillDoc) string {
		var sb strings.Builder
		sb.WriteString(head)
		sb.WriteString("\n\nDomain skill notes:\n")
		for _, s := range subset {
			fmt.Fprintf(&sb, "\n=== %s ===\n%s\n", s.Name, s.Content)
		}
		sb.WriteString("\nWrite the two marked sections now.")
		return sb.String()
	}
	ds := append([]SkillDoc(nil), skills...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })
	if maxBytes <= 0 {
		return build(ds)
	}
	for len(ds) > 1 {
		if msg := build(ds); len(msg) <= maxBytes {
			return msg
		}
		ds = ds[:len(ds)-1]
	}
	return build(ds)
}

// splitArchContext parses a merged response into (architecture, project-context).
// Returns ErrMergedParseFailed if either marker is missing, the architecture body
// is empty, or the context body lacks "## Coding Conventions".
func splitArchContext(raw string) (string, string, error) {
	ai := strings.Index(raw, archCtxMarkerArch)
	ci := strings.Index(raw, archCtxMarkerCtx)
	if ai < 0 || ci < 0 || ci <= ai {
		return "", "", ErrMergedParseFailed
	}
	arch := strings.TrimSpace(raw[ai+len(archCtxMarkerArch) : ci])
	ctxBlock := strings.TrimSpace(raw[ci+len(archCtxMarkerCtx):])
	if arch == "" || !strings.Contains(ctxBlock, "## Coding Conventions") {
		return "", "", ErrMergedParseFailed
	}
	return arch, ctxBlock, nil
}

// GenerateArchitectureAndContext runs one tool-less model turn and returns both
// the architecture skill body and the CLAUDE.md project-context block. On a
// response that cannot be split it returns ErrMergedParseFailed so the caller
// can fall back to GenerateArchitecture + GenerateProjectContext.
func GenerateArchitectureAndContext(ctx context.Context, project string, domains []DomainFact, domainEdges []Edge, skills []SkillDoc, prov provider.Provider, tel *telemetry.Logger, contextSize int) (string, string, error) {
	maxBytes := 0
	if contextSize > 0 {
		if available := contextSize - archCtxReservedOut - archCtxSystemTokens; available > 0 {
			maxBytes = available * 4
		}
	}
	tools := tool.NewRegistry()
	orch := orchestrator.New(prov, tools, tel, BuildArchContextPrompt(), "learn-arch-context")
	raw, err := orch.Chat(ctx, BuildArchContextMessage(project, domains, domainEdges, skills, maxBytes))
	if err != nil {
		return "", "", fmt.Errorf("codegen.GenerateArchitectureAndContext: %w", err)
	}
	arch, ctxBlock, err := splitArchContext(stripCodeFence(raw))
	if err != nil {
		return "", "", fmt.Errorf("codegen.GenerateArchitectureAndContext: %w", err)
	}
	return arch, ctxBlock, nil
}
