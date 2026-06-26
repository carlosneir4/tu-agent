package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

const grepInputSchema = `{
    "type": "object",
    "properties": {
        "pattern": {
            "type": "string",
            "description": "The pattern to search for (grep regex)."
        },
        "path": {
            "type": "string",
            "description": "Directory or file to search in. Defaults to '.' if empty."
        }
    },
    "required": ["pattern"]
}`

// GrepTool searches for a pattern in files recursively using grep.
// When root is non-empty, the search path is confined to that directory.
type GrepTool struct{ root string }

func NewGrepTool(root string) *GrepTool { return &GrepTool{root: root} }

func (t *GrepTool) Name() string { return "grep" }
func (t *GrepTool) Description() string {
	return "Search for a regex pattern in files recursively (-rn)."
}
func (t *GrepTool) InputSchema() json.RawMessage {
	return json.RawMessage(grepInputSchema)
}

type grepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (t *GrepTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in grepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("grep: parsing input: %w", err)
	}
	if in.Pattern == "" {
		return "", fmt.Errorf("grep: pattern is empty")
	}
	searchPath := in.Path
	if searchPath == "" {
		searchPath = "."
	}
	confined, err := ConfinedPath(t.root, searchPath)
	if err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}
	cmd := exec.CommandContext(ctx, "grep", "-rn", in.Pattern, confined) //nolint:gosec
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "(no matches)", nil
		}
		return "", fmt.Errorf("grep: %w", err)
	}
	return string(output), nil
}
