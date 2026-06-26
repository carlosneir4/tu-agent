package subagent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/skill"
	"github.com/tu/tu-agent/internal/subagent"
	"github.com/tu/tu-agent/internal/telemetry"
	"github.com/tu/tu-agent/internal/tool"
)

func newTestDispatchTool(t *testing.T, prov provider.Provider) *subagent.DispatchAgentTool {
	t.Helper()
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFileTool(""))
	registry.Register(tool.NewGrepTool(""))
	registry.Register(tool.NewFindTool(""))
	registry.Register(tool.NewListDirTool())
	registry.Register(tool.NewLoadSkillTool(skill.New(), nil))

	tel, err := telemetry.NewLogger(filepath.Join(t.TempDir(), "tel.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	d := subagent.NewDispatcher(nil, prov, registry, tel, skill.New())
	return subagent.NewDispatchAgentTool(d)
}

func TestDispatchAgentTool_Metadata(t *testing.T) {
	tl := newTestDispatchTool(t, &mockProvider{response: endTurnResponse("ok")})
	if tl.Name() != "dispatch_agent" {
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

func TestDispatchAgentTool_Run_Valid(t *testing.T) {
	prov := &mockProvider{response: endTurnResponse("## Summary\nDone.")}
	tl := newTestDispatchTool(t, prov)

	input, _ := json.Marshal(map[string]string{
		"agent": "codebase-explorer",
		"task":  "find all references to Foo",
	})
	got, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "## Summary\nDone." {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestDispatchAgentTool_Run_UnknownAgent(t *testing.T) {
	tl := newTestDispatchTool(t, &mockProvider{response: endTurnResponse("ok")})
	input, _ := json.Marshal(map[string]string{
		"agent": "nonexistent",
		"task":  "do something",
	})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestDispatchAgentTool_Run_EmptyAgent(t *testing.T) {
	tl := newTestDispatchTool(t, &mockProvider{response: endTurnResponse("ok")})
	input, _ := json.Marshal(map[string]string{"agent": "", "task": "do something"})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty agent name")
	}
}

func TestDispatchAgentTool_Run_EmptyTask(t *testing.T) {
	tl := newTestDispatchTool(t, &mockProvider{response: endTurnResponse("ok")})
	input, _ := json.Marshal(map[string]string{"agent": "codebase-explorer", "task": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty task")
	}
}

func TestDispatchAgentTool_Run_InvalidJSON(t *testing.T) {
	tl := newTestDispatchTool(t, &mockProvider{response: endTurnResponse("ok")})
	_, err := tl.Run(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
