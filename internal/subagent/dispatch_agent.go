package subagent

import (
	"context"
	"encoding/json"
	"fmt"
)

const dispatchAgentInputSchema = `{
    "type": "object",
    "properties": {
        "agent": {
            "type": "string",
            "description": "Name of the sub-agent to dispatch (e.g. 'codebase-explorer')."
        },
        "task": {
            "type": "string",
            "description": "The task to give the sub-agent. Be specific about what to find or analyze."
        }
    },
    "required": ["agent", "task"]
}`

// DispatchAgentTool exposes sub-agent dispatch as a callable tool.
// It lives in internal/subagent/ to avoid an import cycle with internal/tool/.
type DispatchAgentTool struct {
	dispatcher *Dispatcher
}

// NewDispatchAgentTool creates a DispatchAgentTool backed by the given Dispatcher.
func NewDispatchAgentTool(d *Dispatcher) *DispatchAgentTool {
	return &DispatchAgentTool{dispatcher: d}
}

func (t *DispatchAgentTool) Name() string { return "dispatch_agent" }
func (t *DispatchAgentTool) Description() string {
	return "Delegate a task to a specialized sub-agent that runs with a clean context " +
		"and returns a structured summary. Use 'codebase-explorer' for code investigation tasks."
}
func (t *DispatchAgentTool) InputSchema() json.RawMessage {
	return json.RawMessage(dispatchAgentInputSchema)
}

type dispatchInput struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

// Run dispatches the named sub-agent with the task and returns its text response.
func (t *DispatchAgentTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in dispatchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("dispatch_agent: parsing input: %w", err)
	}
	if in.Agent == "" {
		return "", fmt.Errorf("dispatch_agent: agent name is empty")
	}
	if in.Task == "" {
		return "", fmt.Errorf("dispatch_agent: task is empty")
	}
	return t.dispatcher.Dispatch(ctx, in.Agent, in.Task)
}
