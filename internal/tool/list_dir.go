package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const listDirInputSchema = `{
    "type": "object",
    "properties": {
        "path": {
            "type": "string",
            "description": "Absolute or relative path to the directory to list."
        }
    },
    "required": ["path"]
}`

// ListDirTool lists the contents of a directory.
type ListDirTool struct{}

// NewListDirTool returns a new ListDirTool.
func NewListDirTool() *ListDirTool { return &ListDirTool{} }

func (t *ListDirTool) Name() string { return "list_dir" }
func (t *ListDirTool) Description() string {
	return "List the contents of a directory (dirs have trailing '/')."
}
func (t *ListDirTool) InputSchema() json.RawMessage {
	return json.RawMessage(listDirInputSchema)
}

type listDirInput struct {
	Path string `json:"path"`
}

// Run lists the contents of the directory at the given path.
// Directories are suffixed with '/'. Entries are sorted alphabetically.
// An empty directory returns an empty string.
func (t *ListDirTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in listDirInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("list_dir: parsing input: %w", err)
	}
	if in.Path == "" {
		return "", fmt.Errorf("list_dir: path is empty")
	}
	entries, err := os.ReadDir(in.Path)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name()+"/")
		} else {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, "\n"), nil
}
