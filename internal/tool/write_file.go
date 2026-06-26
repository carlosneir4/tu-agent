package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const writeFileInputSchema = `{
    "type": "object",
    "properties": {
        "path": {
            "type": "string",
            "description": "Absolute or relative path to the file to write."
        },
        "content": {
            "type": "string",
            "description": "Content to write. Overwrites if the file exists."
        }
    },
    "required": ["path", "content"]
}`

// WriteFileTool writes content to a file, creating parent directories if needed.
// When root is non-empty, the path is confined to that directory.
type WriteFileTool struct{ root string }

// NewWriteFileTool returns a WriteFileTool confined to root.
// Pass "" to disable confinement.
func NewWriteFileTool(root string) *WriteFileTool { return &WriteFileTool{root: root} }

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file, creating parent directories if needed."
}
func (t *WriteFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(writeFileInputSchema)
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Run writes content to the file at the given path, creating parent directories as needed.
func (t *WriteFileTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in writeFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("write_file: parsing input: %w", err)
	}
	if in.Path == "" {
		return "", fmt.Errorf("write_file: path is empty")
	}
	confined, err := ConfinedPath(t.root, in.Path)
	if err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(confined), 0o755); err != nil {
		return "", fmt.Errorf("write_file: creating directories: %w", err)
	}
	if err := os.WriteFile(confined, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), confined), nil
}
