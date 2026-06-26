package codegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func writeSkill(t *testing.T, root, name, frontmatter, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadIndex_ParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "auth", "name: auth\ndescription: Handles authentication\n", "# Auth\n")
	writeSkill(t, root, "billing", "name: billing\ndescription: Invoices and payments\n", "# Billing\n")

	skills, err := codegen.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name != "auth" || skills[1].Name != "billing" {
		t.Errorf("expected sorted [auth, billing], got [%s, %s]", skills[0].Name, skills[1].Name)
	}
	if skills[0].Description != "Handles authentication" {
		t.Errorf("description mismatch: %q", skills[0].Description)
	}
	if skills[0].Dir != filepath.Join(root, "auth") {
		t.Errorf("Dir mismatch: %q", skills[0].Dir)
	}
}

func TestLoadIndex_SkipsDirsWithoutSkillMD(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "good", "name: good\ndescription: ok\n", "# Good\n")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	skills, err := codegen.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d (empty dir should be skipped)", len(skills))
	}
}

func TestLoadIndex_ReturnsEmptyOnMissingDir(t *testing.T) {
	skills, err := codegen.LoadIndex(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected nil error for missing directory, got: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty index for missing directory, got %d skills", len(skills))
	}
}

func TestLoadIndex_SkipsMalformedFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "bad", "this is not yaml: [\n", "# Bad\n")
	writeSkill(t, root, "good", "name: good\ndescription: ok\n", "# Good\n")
	skills, err := codegen.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex error: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("LoadIndex = %+v, want only the well-formed skill", skills)
	}
}

func TestLoadIndex_LoadsBody(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "auth", "name: auth\ndescription: ok\n", "# Auth\n\nBody.\n")
	skills, err := codegen.LoadIndex(root)
	if err != nil {
		t.Fatalf("LoadIndex error: %v", err)
	}
	if skills[0].Body != "# Auth\n\nBody.\n" {
		t.Errorf("Body content mismatch: %q", skills[0].Body)
	}
}

func TestParseSkillContent(t *testing.T) {
	content := "---\nname: widgets\ndescription: widget rendering\n---\n## body\n- core/Widget.java: the type\n"
	sk, err := codegen.ParseSkillContent(content)
	if err != nil {
		t.Fatalf("ParseSkillContent: %v", err)
	}
	if sk.Name != "widgets" || sk.Description != "widget rendering" {
		t.Errorf("frontmatter not parsed: %+v", sk)
	}
	if !strings.Contains(sk.Body, "core/Widget.java") {
		t.Errorf("body not captured: %q", sk.Body)
	}
	if sk.Dir != "" {
		t.Errorf("Dir should be empty for store-sourced skills, got %q", sk.Dir)
	}
	if _, err := codegen.ParseSkillContent("no frontmatter here"); err == nil {
		t.Errorf("expected error on missing frontmatter")
	}
}
