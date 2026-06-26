package codegen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestLoadTemplate_BaseArchitect(t *testing.T) {
	tmpl, err := codegen.LoadTemplate("base", "architect")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tmpl, "{{.ProjectName}}") {
		t.Error("expected ProjectName variable in template")
	}
	if !strings.Contains(tmpl, "mem_save") {
		t.Error("expected memory instructions in template")
	}
}

func TestLoadTemplate_JavaDeveloper(t *testing.T) {
	tmpl, err := codegen.LoadTemplate("java", "developer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tmpl, "Optional") {
		t.Error("expected Java-specific content (Optional) in java developer template")
	}
}

func TestLoadTemplate_FallsBackToBase(t *testing.T) {
	// "go" language has no templates — must fall back to base
	tmpl, err := codegen.LoadTemplate("go", "architect")
	if err != nil {
		t.Fatalf("expected fallback to base, got error: %v", err)
	}
	if tmpl == "" {
		t.Error("expected non-empty base template as fallback")
	}
}

func TestLoadTemplate_UnknownRoleReturnsError(t *testing.T) {
	_, err := codegen.LoadTemplate("base", "nonexistent-role")
	if err == nil {
		t.Error("expected error for nonexistent role")
	}
}

func TestRenderTemplate_SubstitutesProjectName(t *testing.T) {
	tmpl := `name: "{{.ProjectName}}-developer"`
	data := codegen.AgentTemplateData{ProjectName: "my-svc"}
	got, err := codegen.RenderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "my-svc-developer") {
		t.Errorf("expected ProjectName substitution, got: %s", got)
	}
}

func TestRenderTemplate_EmptyContextRendersBlank(t *testing.T) {
	tmpl := "## Context\n{{.ProjectContext}}\n## End"
	data := codegen.AgentTemplateData{}
	got, err := codegen.RenderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "## Context") || !strings.Contains(got, "## End") {
		t.Error("template structure should render even with empty context")
	}
}

func TestRenderTemplate_AllVariables(t *testing.T) {
	tmpl := "{{.ProjectName}} {{.Language}} {{.BuildTool}} {{.TestCommand}} {{.ProjectContext}}"
	data := codegen.AgentTemplateData{
		ProjectName:    "proj",
		Language:       "java",
		BuildTool:      "maven",
		TestCommand:    "mvn test",
		ProjectContext: "some context",
	}
	got, err := codegen.RenderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "proj java maven mvn test some context" {
		t.Errorf("unexpected rendered output: %q", got)
	}
}

func TestLoadTemplate_GoRolesExist(t *testing.T) {
	// processRoles are language-agnostic; they only have base templates and are
	// exempt from the Go-specific content check.
	processRoles := []string{"analyst", "scribe"}
	for _, role := range codegen.AgentRoles {
		if slices.Contains(processRoles, role) {
			continue
		}
		got, err := codegen.LoadTemplate("go", role)
		if err != nil {
			t.Fatalf("LoadTemplate(go, %q) error: %v", role, err)
		}
		// Go templates must mention a Go-specific command, proving we did not
		// fall through to the generic base template.
		if !strings.Contains(got, "go test") && !strings.Contains(got, "go vet") {
			t.Errorf("role %q: expected Go-specific content, got:\n%s", role, got)
		}
	}
}

func TestAnalystScribeTemplates(t *testing.T) {
	if !slices.Contains(codegen.AgentRoles, "analyst") || !slices.Contains(codegen.AgentRoles, "scribe") {
		t.Fatalf("AgentRoles must include analyst and scribe: %v", codegen.AgentRoles)
	}
	for _, role := range []string{"analyst", "scribe"} {
		for _, lang := range []string{"go", "java", "python", "typescript", "base"} {
			tmpl, err := codegen.LoadTemplate(lang, role)
			if err != nil {
				t.Fatalf("LoadTemplate(%s,%s): %v", lang, role, err)
			}
			out, err := codegen.RenderTemplate(tmpl, codegen.AgentTemplateData{ProjectName: "acme", Language: lang})
			if err != nil {
				t.Fatalf("render %s/%s: %v", lang, role, err)
			}
			if !strings.Contains(out, "acme-"+role) {
				t.Errorf("%s/%s missing name acme-%s", lang, role, role)
			}
			if strings.Contains(out, "{{") {
				t.Errorf("%s/%s has unrendered template tokens", lang, role)
			}
		}
	}
}
