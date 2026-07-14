package codegen_test

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func TestRenderCLAUDEMDTemplate(t *testing.T) {
	got, err := codegen.RenderCLAUDEMDTemplate(codegen.AgentTemplateData{
		ProjectName: "acme", Language: "go", BuildTool: "go", TestCommand: "go test ./...",
	})
	if err != nil {
		t.Fatalf("RenderCLAUDEMDTemplate error: %v", err)
	}
	for _, want := range []string{"acme", "go test ./..."} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered CLAUDE.md missing %q:\n%s", want, got)
		}
	}
}
