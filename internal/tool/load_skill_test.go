package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/skill"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

func makeTestIndex(t *testing.T) *skill.Index {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: go-conventions\ndescription: Go coding conventions\n---\n# Go Conventions\nAlways wrap errors."
	skillDir := filepath.Join(dir, "go-conventions")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := skill.New()
	idx.Add(skill.Entry{Name: "go-conventions", Description: "Go coding conventions", Path: path})
	return idx
}

func TestLoadSkillTool_Metadata(t *testing.T) {
	idx := skill.New()
	tl := tool.NewLoadSkillTool(idx, nil)
	if tl.Name() != "load_skill" {
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

func TestLoadSkillTool_Run_Found(t *testing.T) {
	idx := makeTestIndex(t)
	tl := tool.NewLoadSkillTool(idx, nil)

	input, err := json.Marshal(map[string]string{"name": "go-conventions"})
	if err != nil {
		t.Fatal(err)
	}
	got, runErr := tl.Run(context.Background(), input)
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if got == "" {
		t.Error("expected non-empty content")
	}
	if !strings.Contains(got, "Go Conventions") {
		t.Errorf("expected content to contain 'Go Conventions', got: %q", got)
	}
}

func TestLoadSkillTool_Run_NotFound(t *testing.T) {
	idx := skill.New()
	tl := tool.NewLoadSkillTool(idx, nil)

	input, err := json.Marshal(map[string]string{"name": "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := tl.Run(context.Background(), input)
	if runErr == nil {
		t.Fatal("expected error for missing skill")
	}
}

func TestLoadSkillTool_Run_EmptyName(t *testing.T) {
	idx := skill.New()
	tl := tool.NewLoadSkillTool(idx, nil)

	input, err := json.Marshal(map[string]string{"name": ""})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := tl.Run(context.Background(), input)
	if runErr == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLoadSkillTool_Run_InvalidJSON(t *testing.T) {
	idx := skill.New()
	tl := tool.NewLoadSkillTool(idx, nil)

	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestLoadSkillTool_Run_FileGone(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "gone", Description: "d", Path: "/nonexistent/SKILL.md"})
	tl := tool.NewLoadSkillTool(idx, nil)

	input, err := json.Marshal(map[string]string{"name": "gone"})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := tl.Run(context.Background(), input)
	if runErr == nil {
		t.Fatal("expected error when skill file is missing from disk")
	}
}

func TestLoadSkillLogsTelemetryEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "telemetry.jsonl")
	tel, err := telemetry.NewLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	idx := skill.New()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: demo\ndescription: d\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx.Add(skill.Entry{Name: "demo", Description: "d", Path: path})

	tl := tool.NewLoadSkillTool(idx, tel)
	if _, err := tl.Run(context.Background(), json.RawMessage(`{"name":"demo"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// miss is logged too, with found=false
	_, _ = tl.Run(context.Background(), json.RawMessage(`{"name":"ghost"}`))

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("telemetry lines = %d, want 2", len(lines))
	}
	var e1, e2 telemetry.Entry
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
		t.Fatal(err)
	}
	if e1.Event != "load_skill" || e1.Skill != "demo" || !e1.Found {
		t.Errorf("hit entry = %+v, want event=load_skill skill=demo found=true", e1)
	}
	if e2.Event != "load_skill" || e2.Skill != "ghost" || e2.Found {
		t.Errorf("miss entry = %+v, want event=load_skill skill=ghost found=false", e2)
	}
}

func TestLoadSkillNilLoggerIsSafe(t *testing.T) {
	idx := skill.New()
	tl := tool.NewLoadSkillTool(idx, nil)
	if _, err := tl.Run(context.Background(), json.RawMessage(`{"name":"ghost"}`)); err == nil {
		t.Error("expected not-found error")
	} // must not panic with nil logger
}
