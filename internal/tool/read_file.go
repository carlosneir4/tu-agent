package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const maxReadSize = 1 << 20 // 1 MB

const readFileInputSchema = `{
    "type": "object",
    "properties": {
        "path": {
            "type": "string",
            "description": "Absolute or relative path to the file to read."
        }
    },
    "required": ["path"]
}`

// ReadFileTool reads the full content of a file (max 1 MB).
// When root is non-empty, the path is confined to that directory.
type ReadFileTool struct{ root string }

// NewReadFileTool returns a ReadFileTool confined to root.
// Pass "" to disable confinement.
func NewReadFileTool(root string) *ReadFileTool { return &ReadFileTool{root: root} }

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the full content of a file (max 1 MB)." }
func (t *ReadFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(readFileInputSchema)
}

type readFileInput struct {
	Path string `json:"path"`
}

// Run reads the file at the given path and returns its content as a string.
func (t *ReadFileTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in readFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("read_file: parsing input: %w", err)
	}
	if in.Path == "" {
		return "", fmt.Errorf("read_file: path is empty")
	}
	confined, err := ConfinedPath(t.root, in.Path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	info, err := os.Stat(confined)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	if info.Size() > maxReadSize {
		return "", fmt.Errorf("read_file: %q is %d bytes (max %d)", confined, info.Size(), maxReadSize)
	}
	data, err := os.ReadFile(confined)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	return string(data), nil
}
