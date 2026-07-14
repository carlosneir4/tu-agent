package codegen

import "embed"

//go:embed shells
var shellFS embed.FS

// GenericShell returns the embedded generic (project-agnostic) agent file for a
// role — frontmatter plus a body that instructs the agent to discover the repo
// via get_context/mem_search. It is the loadAgentBody fallback when a repo has
// no materialized .claude/agents/<role>.md, and the source the plugin/agents/
// shells are pinned to. An unknown role returns ("", false).
func GenericShell(role string) (string, bool) {
	data, err := shellFS.ReadFile("shells/" + role + ".md")
	if err != nil {
		return "", false
	}
	return string(data), true
}
