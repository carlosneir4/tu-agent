package codegen

import (
	"strings"

	"github.com/carlosneir4/tu-agent/internal/frontmatter"
)

// AgentTools returns the canonical Claude Code `tools:` line value (the text
// after "tools: ") for a dev-flow agent role, and whether the role is known.
// These mirror plugin/agents/<role>.md verbatim and are pinned by a
// drift test.
func AgentTools(role string) (string, bool) {
	const (
		developer        = "Read, Write, Edit, Grep, Glob, Bash, Skill, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__get_impact, mcp__tu-agent-graph__find_symbol, mcp__tu-agent-graph__mem_save, mcp__tu-agent-graph__mem_search, mcp__tu-agent-graph__mem_recent"
		qa               = "Read, Write, Grep, Glob, Bash, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__get_impact, mcp__tu-agent-graph__find_symbol, mcp__tu-agent-graph__mem_save, mcp__tu-agent-graph__mem_search, mcp__tu-agent-graph__mem_recent"
		design           = "Read, Grep, Glob, Bash, Skill, Write, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__get_impact, mcp__tu-agent-graph__find_symbol, mcp__tu-agent-graph__mem_save, mcp__tu-agent-graph__mem_search, mcp__tu-agent-graph__mem_recent"
		review           = "Read, Grep, Glob, Bash, Skill, Write, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__get_impact, mcp__tu-agent-graph__find_symbol, mcp__tu-agent-graph__mem_search, mcp__tu-agent-graph__mem_recent"
		securityReviewer = "Read, Grep, Glob, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__get_impact, mcp__tu-agent-graph__find_symbol, mcp__tu-agent-graph__mem_search, mcp__tu-agent-graph__mem_recent"
	)
	switch role {
	case "developer":
		return developer, true
	case "qa":
		return qa, true
	case "architect":
		return design, true
	case "pr-reviewer":
		return review, true
	case "security-reviewer":
		return securityReviewer, true
	default:
		return "", false
	}
}

// GraphAgentTools returns the graph/memory MCP tools injected into agents so they
// can query the dependency graph and memory. Single source of truth.
func GraphAgentTools() []string {
	return []string{
		"mcp__tu-agent-graph__get_context",
		"mcp__tu-agent-graph__get_impact",
		"mcp__tu-agent-graph__find_symbol",
		"mcp__tu-agent-graph__mem_save",
		"mcp__tu-agent-graph__mem_search",
		"mcp__tu-agent-graph__mem_recent",
	}
}

// FrontmatterToolsIsInline reports whether the tools: line in the agent's YAML
// frontmatter is in the plain inline scalar form ("tools: A, B, C" or absent).
// It returns (true, true) when the line is inline scalar or there is no tools:
// line at all (UnionFrontmatterTools will safely create one).
// It returns (false, true) when frontmatter is present but the tools: line uses
// a JSON-array form (value starts with "[") or a YAML block-list form (empty
// inline value followed by "- " list items within the frontmatter block).
// It returns (false, false) when no YAML frontmatter is present at all.
func FrontmatterToolsIsInline(content string) (inline bool, hadFrontmatter bool) {
	lines := strings.Split(content, "\n")
	_, end, ok := frontmatter.Bounds(lines)
	if !ok {
		return false, false
	}
	for i := 1; i < end; i++ {
		if strings.HasPrefix(lines[i], "tools:") {
			val := strings.TrimSpace(strings.TrimPrefix(lines[i], "tools:"))
			// JSON array form: value starts with "["
			if strings.HasPrefix(val, "[") {
				return false, true
			}
			// YAML block-list form: empty inline value + next sibling is a "- " item
			if val == "" && i+1 < end {
				next := strings.TrimLeft(lines[i+1], " \t")
				if strings.HasPrefix(next, "- ") {
					return false, true
				}
			}
			// Inline scalar (possibly empty — no tools yet)
			return true, true
		}
	}
	// No tools: line — inline (union will create one safely)
	return true, true
}

// UnionFrontmatterTools appends each tool in add that is not already present to
// the frontmatter `tools:` line of content, preserving existing entries and their
// order. If the frontmatter has no `tools:` line, one is created before the
// closing `---`. Returns the updated content, whether it changed, and whether YAML
// frontmatter was present at all.
func UnionFrontmatterTools(content string, add []string) (out string, changed, had bool) {
	lines := strings.Split(content, "\n")
	_, end, ok := frontmatter.Bounds(lines)
	if !ok {
		return content, false, false
	}
	if len(add) == 0 {
		return content, false, true // frontmatter present, nothing to add
	}
	toolsIdx := -1
	for i := 1; i < end; i++ {
		if strings.HasPrefix(lines[i], "tools:") {
			toolsIdx = i
			break
		}
	}
	if toolsIdx == -1 {
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:end]...)
		newLines = append(newLines, "tools: "+strings.Join(add, ", "))
		newLines = append(newLines, lines[end:]...)
		return strings.Join(newLines, "\n"), true, true
	}
	existing := strings.TrimSpace(strings.TrimPrefix(lines[toolsIdx], "tools:"))
	present := map[string]bool{}
	var parts []string
	if existing != "" {
		for _, t := range strings.Split(existing, ",") {
			if t = strings.TrimSpace(t); t != "" {
				present[t] = true
				parts = append(parts, t)
			}
		}
	}
	added := false
	for _, t := range add {
		if !present[t] {
			parts = append(parts, t)
			present[t] = true
			added = true
		}
	}
	if !added {
		return content, false, true
	}
	lines[toolsIdx] = "tools: " + strings.Join(parts, ", ")
	return strings.Join(lines, "\n"), true, true
}
