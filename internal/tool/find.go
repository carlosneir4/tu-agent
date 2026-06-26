package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

const findInputSchema = `{
    "type": "object",
    "properties": {
        "path": {
            "type": "string",
            "description": "Directory to search in. Defaults to '.' if empty."
        },
        "name": {
            "type": "string",
            "description": "Filename pattern to match (e.g. '*.go', 'config.yaml')."
        }
    },
    "required": ["name"]
}`

// FindTool finds files matching a name pattern under a directory.
// When root is non-empty, the search path is confined to that directory.
type FindTool struct{ root string }

func NewFindTool(root string) *FindTool { return &FindTool{root: root} }

func (t *FindTool) Name() string { return "find" }
func (t *FindTool) Description() string {
	return "Find files matching a name pattern under a directory."
}
func (t *FindTool) InputSchema() json.RawMessage {
	return json.RawMessage(findInputSchema)
}

type findInput struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (t *FindTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in findInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("find: parsing input: %w", err)
	}
	if in.Name == "" {
		return "", fmt.Errorf("find: name pattern is empty")
	}
	searchPath := in.Path
	if searchPath == "" {
		searchPath = "."
	}
	confined, err := ConfinedPath(t.root, searchPath)
	if err != nil {
		return "", fmt.Errorf("find: %w", err)
	}
	cmd := exec.CommandContext(ctx, "find", confined, "-name", in.Name, "-type", "f") //nolint:gosec
	output, err := cmd.Output()
	if err != nil {
		// TODO: find exits non-zero on permission-denied subdirs even when results exist.
		// For Phase 0 (project-scoped searches) this is fine; revisit for broader paths.
		return "", fmt.Errorf("find: %w", err)
	}
	if len(output) == 0 {
		return "(no matches)", nil
	}
	return string(output), nil
}
