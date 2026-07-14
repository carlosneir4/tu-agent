// Package frontmatter is the single, dependency-free implementation of
// "find the leading --- delimited YAML block, split it from the body" used
// by every caller that parses Claude Code-style markdown files (sub-agents,
// skills, generated concept cards). Callers own their own YAML unmarshalling
// into their own struct types — this package only finds the boundaries.
package frontmatter

import "strings"

// Split separates a leading YAML frontmatter block delimited by "---" lines
// from the body. content must open with a "---" line (the very first line,
// after trimming surrounding whitespace); Split then looks for the next line
// that is also "---" and treats everything between the two delimiter lines as
// the frontmatter block, and everything after the closing delimiter as the
// body. CRLF ("\r\n") line endings are handled transparently. A "---" line
// that appears inside the body (after the first closing delimiter) is never
// mistaken for a second closing delimiter, because the scan stops at the
// first match.
//
// ok=false means no well-formed frontmatter block was found — content does
// not start with a "---" line, or no closing "---" line follows it. The
// caller decides whether that means "treat the whole input as body" or
// "this is a malformed file, reject it".
func Split(content string) (fm string, body string, ok bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	_, end, ok := Bounds(lines)
	if !ok {
		return "", "", false
	}
	return strings.Join(lines[1:end], "\n"), strings.Join(lines[end+1:], "\n"), true
}

// SplitLoose is Split's tolerant sibling: it does not require the opening
// "---" to be the very first line. It skips any leading preamble lines (e.g.
// a provenance/HTML-comment line written before the frontmatter block) until
// it finds the first "---" line, then applies the same closing-delimiter
// logic as Split from that point on. It exists for the one caller that
// legitimately needs it — the skill scanner, which indexes crystallized/
// materialized SKILL.md files written as
// "<!-- tu-agent:crystallize ... -->\n---\nname: ...\n---\nbody" (see
// cmd/tu-agent/memory.go saveCrystallizedSkill and `memory materialize`).
// Every other frontmatter caller (sub-agents, generated concept/skill cards)
// legitimately requires the strict "---on line 1" contract and must keep
// using Split/Bounds.
//
// ok=false means no leading "---" line was found anywhere in content, or no
// closing "---" line follows the one that was found.
func SplitLoose(content string) (fm string, body string, ok bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			start = i
			break
		}
	}
	if start == -1 {
		return "", "", false
	}
	_, end, ok := Bounds(lines[start:])
	if !ok {
		return "", "", false
	}
	end += start
	return strings.Join(lines[start+1:end], "\n"), strings.Join(lines[end+1:], "\n"), true
}

// Bounds finds the line indices of a leading frontmatter block in lines (as
// produced by strings.Split(content, "\n")). start is always 0, the index of
// the opening "---" line; end is the index of the first closing "---" line
// found after it (each compared after trimming surrounding whitespace).
// Frontmatter content lives at lines[start+1:end]; the body starts at
// lines[end+1:]. ok=false when lines[0] is not a "---" delimiter or no
// closing delimiter line follows — callers should leave lines untouched.
func Bounds(lines []string) (start, end int, ok bool) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0, 0, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return 0, i, true
		}
	}
	return 0, 0, false
}
