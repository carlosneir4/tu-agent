package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tu/tu-agent/internal/codegen"
)

const (
	graphProtocolOpen  = "<!-- tu-agent:graph-protocol -->"
	graphProtocolClose = "<!-- /tu-agent:graph-protocol -->"
)

const graphProtocolBody = graphProtocolOpen + "\n" +
	"## Graph & memory protocol (tu-agent)\n" +
	"\n" +
	"Before editing a file or answering a question about impact, callers, dependents,\n" +
	"or \"what breaks if I change X\", query the dependency graph FIRST — cheaper and\n" +
	"more complete than re-deriving structure by reading files:\n" +
	"- `get_context(<file-or-symbol>)` — blast radius (dependents) and relevant tests.\n" +
	"- `get_impact(<symbol>)` — what breaks if you change it.\n" +
	"- `find_symbol(<name>)` — locate a symbol across the repo.\n" +
	"If the graph returns \"(none)\" where you expect dependents, cross-check with a\n" +
	"targeted search (it can miss framework/DI/generated edges).\n" +
	"\n" +
	"Memory:\n" +
	"- Starting non-trivial work: `mem_search <area>` (+ `mem_recent`) to recall prior\n" +
	"  decisions, bug-patterns, and gotchas.\n" +
	"- After a decision or a fix worth keeping: `mem_save` with a `decision/...` or\n" +
	"  `bug-pattern/...` topic and a one-paragraph \"why\".\n" +
	graphProtocolClose

// augmentAgents makes every .claude/agents/*.md under root graph-aware: it unions
// the graph MCP tools into each agent's frontmatter tools: line and upserts the
// graph-protocol block into its body. Additive and idempotent; never regenerates a
// body. Agents named in exclude are skipped. A read/write failure aborts the run.
func augmentAgents(root string, exclude map[string]bool) error {
	dir := filepath.Join(root, ".claude", "agents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("no agents found (.claude/agents/ absent)")
			return nil
		}
		return fmt.Errorf("augmentAgents: %w", err)
	}
	devFlow := make(map[string]bool, len(codegen.AgentRoles))
	for _, r := range codegen.AgentRoles {
		devFlow[r] = true
	}
	var specialists []codegen.AgentRef
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		if exclude[stem] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("augmentAgents: reading %s: %w", path, err)
		}
		// A non-dev-flow agent is a domain specialist the generalist dev-flow
		// roles should defer to. Collect it for routing regardless of its tools
		// form (even agents skipped for tool-augmentation can be deferred to).
		if !devFlow[stem] {
			specialists = append(specialists, codegen.AgentRef{
				Name:        stem,
				Description: codegen.FrontmatterField(string(data), "description"),
			})
		}
		inline, had := codegen.FrontmatterToolsIsInline(string(data))
		if !had {
			fmt.Printf("  %s: no frontmatter — skipped\n", e.Name())
			continue
		}
		if !inline {
			fmt.Printf("  %s: tools: uses an array/list form — skipped (only the inline `tools: a, b` form is supported)\n", e.Name())
			continue
		}
		unioned, changed, _ := codegen.UnionFrontmatterTools(string(data), codegen.GraphAgentTools())
		if changed {
			if werr := os.WriteFile(path, []byte(unioned), 0o644); werr != nil {
				return fmt.Errorf("augmentAgents: writing %s: %w", path, werr)
			}
		}
		if werr := upsertMarkedBlock(path, graphProtocolOpen, graphProtocolClose, graphProtocolBody); werr != nil {
			return fmt.Errorf("augmentAgents: %w", werr)
		}
		fmt.Printf("  augmented: .claude/agents/%s\n", e.Name())
		n++
	}
	fmt.Printf("augmented %d agent(s)\n", n)

	// Make the generalist dev-flow roles defer to the repo's specialists, so
	// ad-hoc dispatch routes domain work to the expert instead of the generalist.
	if block := codegen.SpecialistsBlock(specialists); block != "" {
		for _, role := range []string{"developer", "qa"} {
			if exclude[role] {
				continue
			}
			path := filepath.Join(dir, role+".md")
			if _, statErr := os.Stat(path); statErr != nil {
				continue
			}
			if werr := upsertMarkedBlock(path, codegen.SpecialistsOpen, codegen.SpecialistsClose, block); werr != nil {
				return fmt.Errorf("augmentAgents: specialists block %s: %w", path, werr)
			}
			fmt.Printf("  routed %d specialist(s) into .claude/agents/%s.md\n", len(specialists), role)
		}
	}
	return nil
}
