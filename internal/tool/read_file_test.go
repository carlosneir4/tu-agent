package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tool"
)

func TestReadFileTool_Metadata(t *testing.T) {
	tl := tool.NewReadFileTool("")
	if tl.Name() != "read_file" {
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

func TestReadFileTool_Run_Found(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := "hello world\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewReadFileTool("")
	input, _ := json.Marshal(map[string]string{"path": path})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestReadFileTool_Run_NotFound(t *testing.T) {
	tl := tool.NewReadFileTool("")
	input, _ := json.Marshal(map[string]string{"path": "/nonexistent/file.txt"})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFileTool_Run_EmptyPath(t *testing.T) {
	tl := tool.NewReadFileTool("")
	input, _ := json.Marshal(map[string]string{"path": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadFileTool_Run_InvalidJSON(t *testing.T) {
	tl := tool.NewReadFileTool("")
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadFileTool_BlocksOutsideRoot(t *testing.T) {
	root := t.TempDir()
	rt := tool.NewReadFileTool(root)
	input, _ := json.Marshal(map[string]string{"path": "/etc/passwd"})
	_, err := rt.Run(context.Background(), input)
	if err == nil {
		t.Error("expected error reading path outside root")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("expected 'outside' in error, got: %v", err)
	}
}

func TestReadFileTool_AllowsInsideRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := tool.NewReadFileTool(root)
	input, _ := json.Marshal(map[string]string{"path": path})
	out, err := rt.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Errorf("got %q, want hello", out)
	}
}
