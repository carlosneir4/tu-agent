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
		raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agent-templates", role+".md"))
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

func TestDevFlowAgentTddWriteTools(t *testing.T) {
	// Roles the tdd plugin dispatches to write artifacts must carry the plain
	// Claude Code tools, or the plugin path cannot write the .feature/review notes.
	require := map[string][]string{
		"architect":   {"Bash", "Skill", "Write"},
		"developer":   {"Bash", "Edit", "Skill", "Write"},
		"pr-reviewer": {"Bash", "Skill", "Write"},
	}
	for role, want := range require {
		raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agent-templates", role+".md"))
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
