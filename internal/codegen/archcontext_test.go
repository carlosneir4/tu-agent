package codegen

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/provider"
)

// textProvider returns a fixed text block for every Send call.
type textProvider struct{ text string }

func (p *textProvider) Send(ctx context.Context, system string, messages []provider.Message, tools []provider.ToolDef) (provider.Response, error) {
	return provider.Response{
		Blocks:     []provider.Block{{Type: "text", Text: p.text}},
		StopReason: "end_turn",
	}, nil
}
func (p *textProvider) Name() string             { return "text" }
func (p *textProvider) Model() string            { return "text" }
func (p *textProvider) NativeContextWindow() int { return 0 }

func TestGenerateArchitectureAndContext_ParsesBothSections(t *testing.T) {
	resp := "=== ARCHITECTURE ===\n# Architecture\nGlobal map body.\n" +
		"=== PROJECT-CONTEXT ===\n## Coding Conventions\n- wrap errors\n## Key Entry Points\n- main.go — entry\n"
	p := &textProvider{text: resp}
	domains := []DomainFact{{Name: "core", Description: "core", KeyFiles: []string{"a.go"}}}
	skills := []SkillDoc{{Name: "core", Content: "core notes"}}

	arch, ctxBlock, err := GenerateArchitectureAndContext(context.Background(), "proj", domains, nil, skills, p, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(arch, "Global map body") {
		t.Errorf("architecture content wrong: %q", arch)
	}
	if !strings.Contains(ctxBlock, "## Coding Conventions") || !strings.Contains(ctxBlock, "## Key Entry Points") {
		t.Errorf("project-context content wrong: %q", ctxBlock)
	}
}

func TestGenerateArchitectureAndContext_MalformedReturnsSentinel(t *testing.T) {
	// No delimiters: caller must fall back to the two separate calls.
	p := &textProvider{text: "just some prose with no markers"}
	domains := []DomainFact{{Name: "core", Description: "core", KeyFiles: []string{"a.go"}}}
	skills := []SkillDoc{{Name: "core", Content: "core notes"}}

	_, _, err := GenerateArchitectureAndContext(context.Background(), "proj", domains, nil, skills, p, nil, 0)
	if !errors.Is(err, ErrMergedParseFailed) {
		t.Fatalf("want ErrMergedParseFailed, got %v", err)
	}
}

func TestGenerateArchitectureAndContext_EmptyArchBodyReturnsSentinel(t *testing.T) {
	// Marker present but no arch body before the context marker.
	p := &textProvider{text: "=== ARCHITECTURE ===\n=== PROJECT-CONTEXT ===\n## Coding Conventions\n- wrap errors\n"}
	domains := []DomainFact{{Name: "core", Description: "core", KeyFiles: []string{"a.go"}}}
	skills := []SkillDoc{{Name: "core", Content: "core notes"}}

	_, _, err := GenerateArchitectureAndContext(context.Background(), "proj", domains, nil, skills, p, nil, 0)
	if !errors.Is(err, ErrMergedParseFailed) {
		t.Fatalf("want ErrMergedParseFailed for empty arch body, got %v", err)
	}
}

func TestGenerateArchitectureAndContext_MissingConventionsReturnsSentinel(t *testing.T) {
	// Project-context section present but missing the required ## Coding Conventions header.
	p := &textProvider{text: "=== ARCHITECTURE ===\n# Arch\nsome content\n=== PROJECT-CONTEXT ===\n## Key Entry Points\n- main.go — entry\n"}
	domains := []DomainFact{{Name: "core", Description: "core", KeyFiles: []string{"a.go"}}}
	skills := []SkillDoc{{Name: "core", Content: "core notes"}}

	_, _, err := GenerateArchitectureAndContext(context.Background(), "proj", domains, nil, skills, p, nil, 0)
	if !errors.Is(err, ErrMergedParseFailed) {
		t.Fatalf("want ErrMergedParseFailed for missing Coding Conventions, got %v", err)
	}
}
