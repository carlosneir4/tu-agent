package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDevFlowAgentMemoryTools(t *testing.T) {
	const prefix = "mcp__tu-agent-graph__"
	require := map[string][]string{
		"developer":         {"mem_save", "mem_search", "mem_recent"},
		"qa":                {"mem_save", "mem_search", "mem_recent"},
		"architect":         {"mem_save", "mem_search", "mem_recent"},
		"pr-reviewer":       {"mem_search", "mem_recent"},
		"security-reviewer": {"mem_search", "mem_recent"},
		"analyst":           {"mem_save", "mem_search", "mem_recent"},
		"scribe":            {"mem_save", "mem_search", "mem_recent"},
	}
	forbid := map[string][]string{
		"pr-reviewer":       {"mem_save"},
		"security-reviewer": {"mem_save"},
	}
	for role, want := range require {
		raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", role+".md"))
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		body := string(raw)
		for _, tool := range want {
			if !strings.Contains(body, prefix+tool) {
				t.Errorf("%s: missing required tool %s%s", role, prefix, tool)
			}
		}
		for _, tool := range forbid[role] {
			if strings.Contains(body, prefix+tool) {
				t.Errorf("%s: should be recall-only but has %s%s", role, prefix, tool)
			}
		}
	}
}

// TestDevFlowAgentQARunsTests pins that qa carries Bash on the plugin surface:
// its Verify step must be able to run the project's test command, and until
// this matrix fix no surface (AgentTools, plugin skeleton, or codegen
// tool_subset) granted it.
func TestDevFlowAgentQARunsTests(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", "qa.md"))
	if err != nil {
		t.Fatalf("qa: %v", err)
	}
	body := string(raw)
	var toolsLine string
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "tools:") {
			toolsLine = ln
			break
		}
	}
	if toolsLine == "" {
		t.Fatal("qa: no tools: frontmatter line")
	}
	if !regexp.MustCompile(`(^|[ ,])Bash([ ,]|$)`).MatchString(toolsLine) {
		t.Errorf("qa: tools line missing Bash (Verify step must be able to run tests): %s", toolsLine)
	}
}

func TestAgentTemplatesProseWave2(t *testing.T) {
	want := map[string][]string{
		"qa":                {"narrowest package/module test"},
		"security-reviewer": {"cites a real symbol"},
		"developer":         {"standalone work only"},
		"architect":         {"standalone work only"},
		"scribe":            {"never list file paths"},
	}
	for role, phrases := range want {
		raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", role+".md"))
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		body := string(raw)
		for _, phrase := range phrases {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s: missing wave-2 phrase %q", role, phrase)
			}
		}
	}
}

func TestAgentTemplatesDoDGatingWave2(t *testing.T) {
	// The "Record" step of architect/developer templates gates mem_save to
	// standalone work (TDD dispatches let the scribe archive instead). The
	// Definition of Done line must repeat that gate, or a reader following
	// only the DoD checklist would still expect an unconditional mem_save.
	cases := []struct {
		path  string
		phase string // substring anchoring the DoD line itself, not just any mention
	}{
		{
			path:  filepath.Join("..", "..", "internal", "codegen", "templates", "base", "architect.md"),
			phase: "`mem_save` called with topic `decision` (standalone work only",
		},
		{
			path:  filepath.Join("..", "..", "internal", "codegen", "templates", "base", "developer.md"),
			phase: "`mem_save` called when a durable decision was made (standalone work only)",
		},
		{
			path:  filepath.Join("..", "..", "plugin", "agents", "developer.md"),
			phase: "`mem_save` called when a durable decision was made (standalone work only)",
		},
	}
	for _, c := range cases {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		body := string(raw)
		if !strings.Contains(body, "## Definition of done") {
			t.Fatalf("%s: no Definition of done section found", c.path)
		}
		dod := body[strings.Index(body, "## Definition of done"):]
		if !strings.Contains(dod, c.phase) {
			t.Errorf("%s: Definition of done must gate mem_save to standalone work, want substring %q", c.path, c.phase)
		}
	}
}

func TestDevFlowAgentTddWriteTools(t *testing.T) {
	// Roles the tdd plugin dispatches to write artifacts must carry the plain
	// Claude Code tools, or the plugin path cannot write the .feature/review notes.
	require := map[string][]string{
		"architect":   {"Bash", "Skill", "Write"},
		"developer":   {"Bash", "Edit", "Skill", "Write"},
		"pr-reviewer": {"Bash", "Skill", "Write"},
	}
	for role, want := range require {
		raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", role+".md"))
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		// Check only the frontmatter tools: line to avoid matching prose mentions.
		body := string(raw)
		toolsLine := ""
		for _, ln := range strings.Split(body, "\n") {
			if strings.HasPrefix(ln, "tools:") {
				toolsLine = ln
				break
			}
		}
		if toolsLine == "" {
			t.Fatalf("%s: no tools: frontmatter line", role)
		}
		for _, tool := range want {
			// Match as a comma/space-delimited token, not a substring of mcp__ names.
			if !regexp.MustCompile(`(^|[ ,])` + regexp.QuoteMeta(tool) + `([ ,]|$)`).MatchString(toolsLine) {
				t.Errorf("%s: tools line missing %q: %s", role, tool, toolsLine)
			}
		}
	}
}
