package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// architectureMetaKey is the graph-store metadata key under which the generated
// architecture-overview narrative is persisted. The narrative used to live as a
// file (.claude/skills/architecture/SKILL.md); F7-A moved it into the store so
// it is queried the same way as concept cards (get_architecture / get_concept)
// and never drifts as a stale on-disk artifact.
const architectureMetaKey = "architecture_overview"

// normalizeArchitectureNarrative strips the leading YAML frontmatter block and
// the legacy "<!-- tu-agent:generated -->" marker line from a synthesized
// architecture overview, returning just the body. The generator still emits a
// "---\nname: architecture\n---" header (it was a SKILL.md); in the store that
// header is noise, so both write paths (the frozen CLI synthesize and the MCP
// set_architecture tool the plugin uses) run content through here first.
func normalizeArchitectureNarrative(content string) string {
	s := content
	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
			s = s[4+end+len("\n---\n"):]
		}
	}
	s = strings.ReplaceAll(s, "<!-- tu-agent:generated -->\n", "")
	s = strings.ReplaceAll(s, "<!-- tu-agent:generated -->", "")
	return strings.TrimSpace(s)
}

// persistArchitecture normalizes and stores the architecture-overview narrative
// in the graph store's metadata table. An empty (after normalization) narrative
// is a no-op that reports wrote=false, so a failed generation never clobbers a
// good overview with a blank one.
func persistArchitecture(content string) (wrote bool, err error) {
	body := normalizeArchitectureNarrative(content)
	if body == "" {
		return false, nil
	}
	st, err := openGraphStore()
	if err != nil {
		return false, fmt.Errorf("persistArchitecture: %w", err)
	}
	defer st.Close()
	if err := st.SetMeta(architectureMetaKey, body); err != nil {
		return false, fmt.Errorf("persistArchitecture: %w", err)
	}
	return true, nil
}

// loadArchitecture returns the stored architecture-overview narrative ("" if
// none has been synthesized yet).
func loadArchitecture() (string, error) {
	st, err := openGraphStore()
	if err != nil {
		return "", fmt.Errorf("loadArchitecture: %w", err)
	}
	defer st.Close()
	body, err := st.Meta(architectureMetaKey)
	if err != nil {
		return "", fmt.Errorf("loadArchitecture: %w", err)
	}
	return body, nil
}

var graphArchitectureCmd = &cobra.Command{
	Use:   "architecture",
	Short: "Print the synthesized architecture overview from the graph store",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := loadArchitecture()
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "No architecture overview yet — run '/tu-agent:synthesize' (or 'tu-agent learn synthesize') first.")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), body)
		return nil
	},
}

func init() {
	graphCmd.AddCommand(graphArchitectureCmd)
}
