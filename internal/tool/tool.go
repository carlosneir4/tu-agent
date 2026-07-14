package tool

import (
	"context"
	"encoding/json"

	"github.com/carlosneir4/tu-agent/internal/provider"
)

// Tool is a function callable by the agent.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage // valid JSON Schema object
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds the available tools and exposes them as provider.ToolDef slices.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Overwrites any existing tool with the same name.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get returns the tool with the given name, or false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Defs returns all registered tools as provider.ToolDef entries.
func (r *Registry) Defs() []provider.ToolDef {
	defs := make([]provider.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}
