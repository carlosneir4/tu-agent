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

func TestFindTool_Metadata(t *testing.T) {
	tl := tool.NewFindTool("")
	if tl.Name() != "find" {
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

func TestFindTool_Run_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewFindTool("")
	input, _ := json.Marshal(map[string]string{"path": dir, "name": "*.go"})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "main.go") {
		t.Errorf("expected 'main.go' in output, got: %q", got)
	}
	if strings.Contains(got, "other.txt") {
		t.Errorf("expected 'other.txt' to be excluded, got: %q", got)
	}
}

func TestFindTool_Run_NoMatches(t *testing.T) {
	dir := t.TempDir()
	tl := tool.NewFindTool("")
	input, _ := json.Marshal(map[string]string{"path": dir, "name": "*.nonexistent"})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "(no matches)" {
		t.Errorf("expected '(no matches)', got %q", got)
	}
}

func TestFindTool_Run_EmptyName(t *testing.T) {
	tl := tool.NewFindTool("")
	input, _ := json.Marshal(map[string]string{"name": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFindTool_Run_InvalidJSON(t *testing.T) {
	tl := tool.NewFindTool("")
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
