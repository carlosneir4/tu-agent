package codegen

import "strings"

// stripCodeFence removes a single Markdown code fence wrapping the whole string:
// a leading ``` line (optionally with a language tag) and a trailing ``` line.
// Models often wrap a SKILL.md in a fence; left in place it pushes the YAML
// frontmatter off the first line. Content without a leading fence is returned
// trimmed but otherwise unchanged.
func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	lines := strings.Split(t, "\n")
	lines = lines[1:] // drop opening fence (``` or ```lang)
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1] // drop closing fence
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
