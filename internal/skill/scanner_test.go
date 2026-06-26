package skill

// scanner_test.go uses package skill (white-box) to test the unexported parseFrontmatter function.
// skill_test.go uses package skill_test (black-box) for the exported Index API.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter_Valid(t *testing.T) {
	content := "---\nname: test-skill\ndescription: a test skill\ntriggers:\n  - test\n  - demo\n---\n# Body here"
	e, err := parseFrontmatter("/fake/SKILL.md", strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Name != "test-skill" {
		t.Errorf("name: got %q, want %q", e.Name, "test-skill")
	}
	if e.Description != "a test skill" {
		t.Errorf("description: got %q, want %q", e.Description, "a test skill")
	}
	if len(e.Triggers) != 2 || e.Triggers[0] != "test" || e.Triggers[1] != "demo" {
		t.Errorf("triggers: got %v", e.Triggers)
	}
	if e.Path != "/fake/SKILL.md" {
		t.Errorf("path: got %q", e.Path)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Just a plain markdown file\nNo frontmatter here."
	e, err := parseFrontmatter("/fake/SKILL.md", strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Name != "" {
		t.Errorf("expected empty name for file without frontmatter, got %q", e.Name)
	}
}

func TestParseFrontmatter_NoTriggers(t *testing.T) {
	content := "---\nname: minimal\ndescription: just the basics\n---\nBody."
	e, err := parseFrontmatter("/fake/SKILL.md", strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Name != "minimal" {
		t.Errorf("name: got %q", e.Name)
	}
	if len(e.Triggers) != 0 {
		t.Errorf("expected no triggers, got %v", e.Triggers)
	}
}

func TestParseFrontmatter_UnclosedFrontmatter(t *testing.T) {
	content := "---\nname: broken\ndescription: no closing delimiter"
	e, err := parseFrontmatter("/fake/SKILL.md", strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Name != "" {
		t.Errorf("expected empty entry for unclosed frontmatter, got name=%q", e.Name)
	}
}

func TestSearchPaths(t *testing.T) {
	paths := SearchPaths("/home/user", "/repo")
	if len(paths) != 4 {
		t.Fatalf("expected 4 paths, got %d", len(paths))
	}
	if paths[0] != "/home/user/.claude/skills" {
		t.Errorf("path[0]: got %q", paths[0])
	}
	if paths[1] != "/home/user/.tu-agent/skills" {
		t.Errorf("path[1]: got %q", paths[1])
	}
	if paths[2] != "/repo/.claude/skills" {
		t.Errorf("path[2]: got %q", paths[2])
	}
	if paths[3] != "/repo/.tu-agent/skills" {
		t.Errorf("path[3]: got %q", paths[3])
	}
}

func TestSearchPaths_EmptyHome(t *testing.T) {
	paths := SearchPaths("", "/repo")
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths when home is empty, got %d", len(paths))
	}
	if paths[0] != "/repo/.claude/skills" {
		t.Errorf("path[0]: got %q", paths[0])
	}
	if paths[1] != "/repo/.tu-agent/skills" {
		t.Errorf("path[1]: got %q", paths[1])
	}
}

func TestScan_Empty(t *testing.T) {
	dir := t.TempDir()
	idx, err := Scan([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("expected empty index, got %d entries", idx.Len())
	}
}

func TestScan_WithSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "bash-helper", "---\nname: bash-helper\ndescription: runs bash scripts\n---\nBody.")
	writeSkill(t, dir, "api-client", "---\nname: api-client\ndescription: calls REST APIs\n---\nBody.")

	idx, err := Scan([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", idx.Len())
	}
	e, ok := idx.Get("bash-helper")
	if !ok {
		t.Fatal("bash-helper not found")
	}
	if e.Description != "runs bash scripts" {
		t.Errorf("description: got %q", e.Description)
	}
}

func TestScan_LaterDirOverrides(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeSkill(t, dir1, "my-skill", "---\nname: my-skill\ndescription: from dir1\n---\n")
	writeSkill(t, dir2, "my-skill", "---\nname: my-skill\ndescription: from dir2\n---\n")

	idx, err := Scan([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e, _ := idx.Get("my-skill")
	if e.Description != "from dir2" {
		t.Errorf("expected dir2 to win, got %q", e.Description)
	}
}

func TestScan_SkipsInvalidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "valid-skill", "---\nname: valid-skill\ndescription: ok\n---\nBody.")
	writeSkill(t, dir, "no-frontmatter", "# No frontmatter here\nJust body.")

	idx, err := Scan([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.Len() != 1 {
		t.Errorf("expected 1 valid entry, got %d", idx.Len())
	}
	if _, ok := idx.Get("valid-skill"); !ok {
		t.Error("valid-skill not found")
	}
}

// writeSkill creates <dir>/<name>/SKILL.md with the given content.
func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
