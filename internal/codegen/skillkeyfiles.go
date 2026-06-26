package codegen

import (
	"sort"
	"strings"
)

// ParseKeyFiles extracts file paths under the "## Key Files" section of a
// SKILL.md body. Recognizes "- <path>: <why>" and "- <path>"; surrounding
// backticks are stripped; parsing stops at the next "## " header.
func ParseKeyFiles(body string) []string {
	var out []string
	inSection := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			inSection = strings.EqualFold(strings.TrimSpace(line[3:]), "Key Files")
			continue
		}
		if !inSection || !strings.HasPrefix(line, "- ") {
			continue
		}
		item := strings.TrimSpace(line[2:])
		if i := strings.Index(item, ":"); i >= 0 {
			item = strings.TrimSpace(item[:i])
		}
		item = strings.Trim(item, "`")
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// BuildFileToDomain maps each Key File path to its owning skill Name. On
// collision the first skill by sorted Name wins (deterministic).
func BuildFileToDomain(skills []Skill) map[string]string {
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	out := make(map[string]string)
	for _, s := range sorted {
		for _, f := range ParseKeyFiles(s.Body) {
			if _, exists := out[f]; !exists {
				out[f] = s.Name
			}
		}
	}
	return out
}
