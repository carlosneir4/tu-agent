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

func TestGrepTool_Metadata(t *testing.T) {
	tl := tool.NewGrepTool("")
	if tl.Name() != "grep" {
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

func TestGrepTool_Run_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\nfoo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewGrepTool("")
	input, _ := json.Marshal(map[string]string{"pattern": "hello", "path": dir})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", got)
	}
}

func TestGrepTool_Run_NoMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("no match here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewGrepTool("")
	input, _ := json.Marshal(map[string]string{"pattern": "zzznomatch", "path": dir})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error for no matches: %v", err)
	}
	if got != "(no matches)" {
		t.Errorf("expected '(no matches)', got %q", got)
	}
}

func TestGrepTool_Run_DefaultsToCurrentDir(t *testing.T) {
	// Create an empty temp dir so grep searches there (via path="") and finds nothing.
	// We cannot rely on "." because the test source itself would match many patterns.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stub.txt"), []byte("stub content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Change working directory to our controlled temp dir for this test.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	tl := tool.NewGrepTool("")
	// Pattern that is not in stub.txt — omit "path" to trigger the default "." logic.
	input, _ := json.Marshal(map[string]string{"pattern": "nomatch_unique_99zz"})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error with default path: %v", err)
	}
	if got != "(no matches)" {
		t.Errorf("expected '(no matches)', got %q", got)
	}
}

func TestGrepTool_Run_EmptyPattern(t *testing.T) {
	tl := tool.NewGrepTool("")
	input, _ := json.Marshal(map[string]string{"pattern": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestGrepTool_Run_InvalidJSON(t *testing.T) {
	tl := tool.NewGrepTool("")
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
