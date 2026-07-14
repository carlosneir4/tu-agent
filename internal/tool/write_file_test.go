package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tool"
)

func TestWriteFileTool_Metadata(t *testing.T) {
	tl := tool.NewWriteFileTool("")
	if tl.Name() != "write_file" {
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

func TestWriteFileTool_Run_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	tl := tool.NewWriteFileTool("")
	input, _ := json.Marshal(map[string]string{"path": path, "content": "hello"})
	_, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content: got %q, want %q", string(data), "hello")
	}
}

func TestWriteFileTool_Run_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "file.txt")
	tl := tool.NewWriteFileTool("")
	input, _ := json.Marshal(map[string]string{"path": path, "content": "data"})
	_, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created at nested path: %v", err)
	}
}

func TestWriteFileTool_Run_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewWriteFileTool("")
	input, _ := json.Marshal(map[string]string{"path": path, "content": "new"})
	if _, err := tl.Run(context.Background(), input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("expected overwrite: got %q", string(data))
	}
}

func TestWriteFileTool_Run_EmptyPath(t *testing.T) {
	tl := tool.NewWriteFileTool("")
	input, _ := json.Marshal(map[string]string{"path": "", "content": "x"})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestWriteFileTool_Run_InvalidJSON(t *testing.T) {
	tl := tool.NewWriteFileTool("")
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWriteFileTool_BlocksOutsideRoot(t *testing.T) {
	root := t.TempDir()
	wt := tool.NewWriteFileTool(root)
	input, _ := json.Marshal(map[string]any{"path": "/tmp/evil.sh", "content": "evil"})
	_, err := wt.Run(context.Background(), input)
	if err == nil {
		t.Error("expected error writing outside root")
	}
}

func TestWriteFileTool_AllowsInsideRoot(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "out.txt")
	wt := tool.NewWriteFileTool(root)
	input, _ := json.Marshal(map[string]any{"path": dest, "content": "hello"})
	_, err := wt.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "hello" {
		t.Errorf("unexpected content: %q", data)
	}
}
