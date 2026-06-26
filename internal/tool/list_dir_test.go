package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/tool"
)

func TestListDirTool_Metadata(t *testing.T) {
	tl := tool.NewListDirTool()
	if tl.Name() != "list_dir" {
		t.Errorf("Name: got %q", tl.Name())
	}
	if tl.Description() == "" {
		t.Error("Description should not be empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(tl.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

func TestListDirTool_Run_ListsEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewListDirTool()
	input, _ := json.Marshal(map[string]string{"path": dir})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(lines), lines)
	}
	if lines[0] != "a.txt" {
		t.Errorf("expected sorted first entry 'a.txt', got %q", lines[0])
	}
	if lines[1] != "b.txt" {
		t.Errorf("expected 'b.txt', got %q", lines[1])
	}
	if lines[2] != "subdir/" {
		t.Errorf("expected 'subdir/' with trailing slash, got %q", lines[2])
	}
}

func TestListDirTool_Run_Empty(t *testing.T) {
	dir := t.TempDir()
	tl := tool.NewListDirTool()
	input, _ := json.Marshal(map[string]string{"path": dir})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for empty dir, got %q", got)
	}
}

func TestListDirTool_Run_NotFound(t *testing.T) {
	tl := tool.NewListDirTool()
	input, _ := json.Marshal(map[string]string{"path": "/nonexistent/dir"})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestListDirTool_Run_EmptyPath(t *testing.T) {
	tl := tool.NewListDirTool()
	input, _ := json.Marshal(map[string]string{"path": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestListDirTool_Run_InvalidJSON(t *testing.T) {
	tl := tool.NewListDirTool()
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
