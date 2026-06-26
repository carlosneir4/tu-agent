package codegen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/telemetry"
)

type recordProvider struct {
	reply   string
	prompts []string
}

func (r *recordProvider) Name() string             { return "mock" }
func (r *recordProvider) Model() string            { return "mock" }
func (r *recordProvider) NativeContextWindow() int { return 200000 }
func (r *recordProvider) Send(_ context.Context, _ string, msgs []provider.Message, _ []provider.ToolDef) (provider.Response, error) {
	for _, m := range msgs {
		for _, b := range m.Blocks {
			if b.Type == "text" {
				r.prompts = append(r.prompts, b.Text)
			}
		}
	}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: r.reply}}, StopReason: "end_turn"}, nil
}

func newDomaingenTelemetry(t *testing.T) *telemetry.Logger {
	t.Helper()
	tel, err := telemetry.NewLogger(t.TempDir() + "/telemetry.jsonl")
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	return tel
}

func TestGenerateProjectContext_ReturnsSections(t *testing.T) {
	rp := &recordProvider{reply: "## Coding Conventions\n- wrap errors\n\n## Key Entry Points\n- main.go — entry"}
	skills := []codegen.SkillDoc{{Name: "feed", Content: "patterns"}}
	out, err := codegen.GenerateProjectContext(context.Background(), "proj", skills, rp, newDomaingenTelemetry(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Coding Conventions") || !strings.Contains(out, "## Key Entry Points") {
		t.Errorf("missing sections: %q", out)
	}
}

func TestGenerateProjectContext_RejectsEmpty(t *testing.T) {
	rp := &recordProvider{reply: "```"}
	skills := []codegen.SkillDoc{{Name: "feed", Content: "x"}}
	if _, err := codegen.GenerateProjectContext(context.Background(), "proj", skills, rp, newDomaingenTelemetry(t), 0); err == nil {
		t.Fatal("expected error for output missing required sections")
	}
}

func TestGenerateProjectContext_StripsFence(t *testing.T) {
	rp := &recordProvider{reply: "```markdown\n## Coding Conventions\n- x\n\n## Key Entry Points\n- y\n```"}
	skills := []codegen.SkillDoc{{Name: "feed", Content: "x"}}
	out, err := codegen.GenerateProjectContext(context.Background(), "proj", skills, rp, newDomaingenTelemetry(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "## Coding Conventions") {
		t.Errorf("fence not stripped: %q", out)
	}
}
