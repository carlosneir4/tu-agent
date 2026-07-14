package subagent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/subagent"
)

func writeAgent(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPaths(t *testing.T) {
	paths := subagent.SearchPaths("/home/user", "")
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
	if paths[0] != "/home/user/.claude/agents" {
		t.Errorf("path[0]: got %q", paths[0])
	}
	if paths[1] != "/home/user/.tu-agent/sub-agents" {
		t.Errorf("path[1]: got %q", paths[1])
	}
}

func TestSearchPaths_IncludesCWDClaudeAgents(t *testing.T) {
	paths := subagent.SearchPaths("/home/user", "/repo")
	found := false
	for _, p := range paths {
		if strings.Contains(p, "/repo") && strings.HasSuffix(p, ".claude/agents") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected <cwd>/.claude/agents in paths, got: %v", paths)
	}
}

func TestSearchPaths_EmptyCWDOmitsCWDPaths(t *testing.T) {
	paths := subagent.SearchPaths("/home/user", "")
	for _, p := range paths {
		if !strings.HasPrefix(p, "/home/user") {
			t.Errorf("expected only home-relative paths when cwd is empty, got: %v", p)
		}
	}
}

func TestSearchPaths_CWDAgentsLoadedLast(t *testing.T) {
	paths := subagent.SearchPaths("/home/user", "/repo")
	var homeIdx, cwdIdx int
	for i, p := range paths {
		if strings.HasPrefix(p, "/home/user") {
			homeIdx = i
		}
		if strings.HasPrefix(p, "/repo") {
			cwdIdx = i
		}
	}
	if cwdIdx <= homeIdx {
		t.Errorf("expected cwd paths after home paths for correct override precedence, got: %v", paths)
	}
}

func TestLoad_Empty(t *testing.T) {
	dir := t.TempDir()
	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected 0 definitions, got %d", len(defs))
	}
}

func TestLoad_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-agent\ndescription: does stuff\ndefault_model: local\ntool_subset:\n  - read_file\n  - grep\n---\nYou are my-agent. Explore code."
	writeAgent(t, dir, "my-agent", content)

	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	d := defs[0]
	if d.Name != "my-agent" {
		t.Errorf("Name: got %q", d.Name)
	}
	if d.Description != "does stuff" {
		t.Errorf("Description: got %q", d.Description)
	}
	if d.DefaultModel != "local" {
		t.Errorf("DefaultModel: got %q", d.DefaultModel)
	}
	if len(d.ToolSubset) != 2 || d.ToolSubset[0] != "read_file" || d.ToolSubset[1] != "grep" {
		t.Errorf("ToolSubset: got %v", d.ToolSubset)
	}
	if d.SystemPrompt != "You are my-agent. Explore code." {
		t.Errorf("SystemPrompt: got %q", d.SystemPrompt)
	}
}

func TestLoad_SkipsNoName(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "no-name", "---\ndescription: no name here\n---\nBody.")
	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected file without name to be skipped, got %d defs", len(defs))
	}
}

func TestLoad_SkipsNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "plain", "# Just markdown\nNo frontmatter.")
	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected file without frontmatter to be skipped, got %d defs", len(defs))
	}
}

func TestLoad_LaterDirOverrides(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeAgent(t, dir1, "my-agent", "---\nname: my-agent\ndescription: from dir1\n---\nBody1.")
	writeAgent(t, dir2, "my-agent", "---\nname: my-agent\ndescription: from dir2\n---\nBody2.")

	defs, err := subagent.Load([]string{dir1, dir2}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Description != "from dir2" {
		t.Errorf("expected dir2 to win, got %q", defs[0].Description)
	}
}

func TestLoad_NonExistentDir(t *testing.T) {
	defs, err := subagent.Load([]string{"/nonexistent/path"}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected 0 defs for missing dir, got %d", len(defs))
	}
}

func TestLoad_ProjectLocalAgentsGetReadOnlyTools(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: evil-agent\ndescription: test\ndefault_model: claude\ntool_subset:\n  - bash\n  - write_file\n---\nDo bad things.\n"
	if err := os.WriteFile(filepath.Join(dir, "evil.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	readOnlyDirs := map[string]bool{filepath.Clean(dir): true}
	defs, err := subagent.Load([]string{dir}, readOnlyDirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	for _, tool := range defs[0].ToolSubset {
		if tool == "bash" || tool == "write_file" {
			t.Errorf("project-local agent must not have tool %q", tool)
		}
	}
}

// TestLoad_UnterminatedFrontmatterSkipped is a regression test for a bug where
// a file that opens frontmatter with "---" but never closes it silently
// loaded a *Definition with Name set but an EMPTY SystemPrompt: the
// line-scanner never flipped out of "in frontmatter" mode, so every
// remaining line (the would-be system prompt) was swallowed into the
// frontmatter buffer instead of the body buffer. The agent then loaded and
// dispatched with no instructions at all. Unterminated frontmatter must be
// treated as malformed — the same as having no opening delimiter — and the
// file must be skipped entirely, not loaded with an empty prompt.
func TestLoad_UnterminatedFrontmatterSkipped(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: broken-agent\ndescription: no closing delimiter here at all\n"
	writeAgent(t, dir, "broken-agent", content)

	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected unterminated-frontmatter agent to be skipped, got %d defs (bug: agent loaded with empty SystemPrompt): %+v", len(defs), defs)
	}
}

func TestLoad_TrustedAgentsKeepDeclaredTools(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: trusted-agent\ndescription: test\ndefault_model: claude\ntool_subset:\n  - bash\n  - write_file\n---\nDo things.\n"
	if err := os.WriteFile(filepath.Join(dir, "trusted.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// dir NOT in readOnlyDirs — trusted
	defs, err := subagent.Load([]string{dir}, map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	found := false
	for _, tool := range defs[0].ToolSubset {
		if tool == "bash" {
			found = true
		}
	}
	if !found {
		t.Error("trusted agent should keep its declared bash tool")
	}
}
