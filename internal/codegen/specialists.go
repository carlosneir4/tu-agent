package codegen

import (
	"fmt"
	"strings"
)

// Markers delimit the specialist-routing block inserted into the generalist
// dev-flow agents (developer, qa). Re-running augment rewrites between them, so
// the block stays idempotent and never duplicates.
const (
	SpecialistsOpen  = "<!-- tu-agent:specialists -->"
	SpecialistsClose = "<!-- /tu-agent:specialists -->"
)

// AgentRef is a sub-agent's name (filename stem) and its frontmatter description.
type AgentRef struct {
	Name        string
	Description string
}

// FrontmatterField returns the value of a single-line scalar field in the YAML
// frontmatter (the first `---`-delimited block). Surrounding single or double
// quotes are stripped. It returns "" when there is no frontmatter or the field
// is absent.
func FrontmatterField(content, field string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end >= 0 {
		rest = rest[:end]
	}
	prefix := field + ":"
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		val := strings.TrimSpace(trimmed[len(prefix):])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		return val
	}
	return ""
}

// SpecialistsBlock builds the marker-delimited routing block that tells a
// generalist dev-flow agent to defer to the repo's domain specialists. It
// returns "" when there are no specialists, so callers can skip the upsert.
func SpecialistsBlock(specialists []AgentRef) string {
	if len(specialists) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(SpecialistsOpen + "\n")
	sb.WriteString("## Defer to domain specialists\n\n")
	sb.WriteString("This repo has specialist sub-agents. For a task squarely in one's domain,\n")
	sb.WriteString("prefer dispatching it to that specialist instead of handling it here:\n")
	for _, s := range specialists {
		if strings.TrimSpace(s.Description) == "" {
			fmt.Fprintf(&sb, "- `%s`\n", s.Name)
			continue
		}
		fmt.Fprintf(&sb, "- `%s` — %s\n", s.Name, s.Description)
	}
	sb.WriteString(SpecialistsClose)
	return sb.String()
}
