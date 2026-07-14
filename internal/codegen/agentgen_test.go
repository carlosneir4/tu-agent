package codegen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
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

// variantRoles and supportedLangs are declared in overlay_test.go (same
// codegen_test package): the roles that specialize per language via the runtime
// overlay, and the languages that have variant overlays. Their body templates
// now all resolve to the generic base body.

// @s2 (java analog) — LoadTemplate("java","developer") resolves the generic
// base body, byte-identical to LoadTemplate("base","developer"), and carries no
// language-specific content (that now lives in LangOverlay, covered by
// overlay_test.go).
func TestLoadTemplate_JavaDeveloper(t *testing.T) {
	tmpl, err := codegen.LoadTemplate("java", "developer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base, err := codegen.LoadTemplate("base", "developer")
	if err != nil {
		t.Fatalf("unexpected error loading base: %v", err)
	}
	if tmpl != base {
		t.Errorf("java developer template must equal base developer template after per-lang removal")
	}
	if strings.Contains(tmpl, "Java-specific") {
		t.Errorf("java developer body must not contain %q — that content now lives in the overlay", "Java-specific")
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

// @s2/@s3 — for every variant role, LoadTemplate("go",role) resolves the
// generic base body (byte-identical to base) and carries no Go-specific
// section. The Go specialization now lives only in LangOverlay.
func TestLoadTemplate_GoRolesResolveBase(t *testing.T) {
	forbidden := []string{"Go-specific", "Go architecture", "Go testing"}
	for _, role := range variantRoles {
		got, err := codegen.LoadTemplate("go", role)
		if err != nil {
			t.Fatalf("LoadTemplate(go, %q) error: %v", role, err)
		}
		base, err := codegen.LoadTemplate("base", role)
		if err != nil {
			t.Fatalf("LoadTemplate(base, %q) error: %v", role, err)
		}
		if got != base {
			t.Errorf("role %q: go template must equal base template after per-lang removal", role)
		}
		for _, f := range forbidden {
			if strings.Contains(got, f) {
				t.Errorf("role %q: go body must not contain %q — that content now lives in the overlay", role, f)
			}
		}
	}
}

// @s3 — LoadTemplate(lang,"qa") is byte-identical across every supported
// language and equals the base qa template. Proves the per-lang body tree is
// gone and all languages collapse onto base.
func TestLoadTemplate_QAIdenticalAcrossLangs(t *testing.T) {
	base, err := codegen.LoadTemplate("base", "qa")
	if err != nil {
		t.Fatalf("LoadTemplate(base, qa) error: %v", err)
	}
	for _, lang := range supportedLangs {
		got, err := codegen.LoadTemplate(lang, "qa")
		if err != nil {
			t.Fatalf("LoadTemplate(%q, qa) error: %v", lang, err)
		}
		if got != base {
			t.Errorf("LoadTemplate(%q, qa) must be byte-identical to base qa template", lang)
		}
	}
}

// @s1 — the per-language body template directories are absent from the embedded
// FS. templateFS is unexported, so this is asserted behaviorally: for every
// variant role, each supported language resolves to the exact base body (if a
// templates/<lang>/<role>.md still existed it would win over base and differ).
// @s5 — the resolved (and therefore materialized) bodies carry no
// "## <Lang>-specific" section for any role.
func TestLoadTemplate_PerLangDirsAbsent(t *testing.T) {
	forbiddenSections := []string{
		"## Go-specific", "## Java-specific", "## Python-specific", "## TypeScript-specific",
		"## Go architecture", "## Go testing",
	}
	for _, role := range variantRoles {
		base, err := codegen.LoadTemplate("base", role)
		if err != nil {
			t.Fatalf("LoadTemplate(base, %q) error: %v", role, err)
		}
		for _, lang := range supportedLangs {
			got, err := codegen.LoadTemplate(lang, role)
			if err != nil {
				t.Fatalf("LoadTemplate(%q, %q) error: %v", lang, role, err)
			}
			if got != base {
				t.Errorf("LoadTemplate(%q, %q) != base: a per-lang template directory still shadows base", lang, role)
			}
			// Post-render body (generateAgents renders LoadTemplate output) must
			// carry no language-specific section.
			rendered, rerr := codegen.RenderTemplate(got, codegen.AgentTemplateData{ProjectName: "acme", Language: lang})
			if rerr != nil {
				t.Fatalf("RenderTemplate(%q, %q): %v", lang, role, rerr)
			}
			for _, f := range forbiddenSections {
				if strings.Contains(rendered, f) {
					t.Errorf("materialized %q/%q body contains language-specific section %q", lang, role, f)
				}
			}
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
