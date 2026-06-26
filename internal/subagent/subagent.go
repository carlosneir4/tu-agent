package subagent

import (
	"context"
	"fmt"

	"github.com/tu/tu-agent/internal/orchestrator"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/skill"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

// Definition describes a sub-agent: its system prompt, preferred provider model,
// and the subset of tools it is allowed to use.
type Definition struct {
	Name         string
	Description  string
	DefaultModel string
	ToolSubset   []string
	SystemPrompt string
}

// Dispatcher holds available sub-agent definitions and the shared resources needed
// to run them.
type Dispatcher struct {
	defs      map[string]*Definition
	provider  provider.Provider
	allTools  *tool.Registry
	telemetry *telemetry.Logger
	skillIdx  *skill.Index
}

// NewDispatcher creates a Dispatcher. Built-in definitions are always included;
// userDefs override them by name when the same name appears in both.
func NewDispatcher(
	userDefs []*Definition,
	p provider.Provider,
	tools *tool.Registry,
	tel *telemetry.Logger,
	idx *skill.Index,
) *Dispatcher {
	defs := make(map[string]*Definition)
	for _, d := range BuiltinDefs() {
		defs[d.Name] = d
	}
	for _, d := range userDefs {
		defs[d.Name] = d
	}
	return &Dispatcher{
		defs:      defs,
		provider:  p,
		allTools:  tools,
		telemetry: tel,
		skillIdx:  idx,
	}
}

// Dispatch runs the named sub-agent with the given task description and returns
// its text response. The sub-agent runs in a clean context with a restricted
// tool subset. Only the final text is returned to the caller.
func (d *Dispatcher) Dispatch(ctx context.Context, agentName, task string) (string, error) {
	def, ok := d.defs[agentName]
	if !ok {
		return "", fmt.Errorf("subagent.Dispatch: unknown sub-agent %q", agentName)
	}
	restricted := buildRestrictedRegistry(d.allTools, def.ToolSubset)
	systemPrompt := buildSubAgentPrompt(def, d.skillIdx)
	orch := orchestrator.New(d.provider, restricted, d.telemetry, systemPrompt, def.Name)
	return orch.Chat(ctx, task)
}

// AgentNames returns the names of all known sub-agent definitions.
func (d *Dispatcher) AgentNames() []string {
	names := make([]string, 0, len(d.defs))
	for name := range d.defs {
		names = append(names, name)
	}
	return names
}

// Len returns the number of known sub-agent definitions.
func (d *Dispatcher) Len() int { return len(d.defs) }

func buildRestrictedRegistry(all *tool.Registry, subset []string) *tool.Registry {
	restricted := tool.NewRegistry()
	for _, name := range subset {
		if t, ok := all.Get(name); ok {
			restricted.Register(t)
		}
	}
	return restricted
}

func buildSubAgentPrompt(def *Definition, idx *skill.Index) string {
	prompt := def.SystemPrompt
	summary := idx.Summary()
	if summary == "" {
		return prompt
	}
	return prompt + "\n\n## Available Skills\n\n" +
		"Use the load_skill tool to load the full content of a skill when relevant.\n\n" +
		summary
}
