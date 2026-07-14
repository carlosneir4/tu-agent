package subagent_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/skill"
	"github.com/carlosneir4/tu-agent/internal/subagent"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

type mockProvider struct {
	response provider.Response
	err      error
	lastDefs []provider.ToolDef
}

func (m *mockProvider) Send(_ context.Context, _ string, _ []provider.Message, defs []provider.ToolDef) (provider.Response, error) {
	m.lastDefs = defs
	return m.response, m.err
}
func (m *mockProvider) Name() string             { return "mock" }
func (m *mockProvider) Model() string            { return "mock-model" }
func (m *mockProvider) NativeContextWindow() int { return 200000 }

func newTestDispatcher(t *testing.T, p provider.Provider, userDefs []*subagent.Definition) *subagent.Dispatcher {
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
	return subagent.NewDispatcher(userDefs, p, registry, tel, skill.New())
}

func endTurnResponse(text string) provider.Response {
	return provider.Response{
		Blocks:     []provider.Block{{Type: "text", Text: text}},
		StopReason: "end_turn",
	}
}

func TestDispatcher_BuiltinsAlwaysPresent(t *testing.T) {
	prov := &mockProvider{response: endTurnResponse("ok")}
	d := newTestDispatcher(t, prov, nil)
	if d.Len() == 0 {
		t.Fatal("expected at least one built-in definition")
	}
	found := false
	for _, name := range d.AgentNames() {
		if name == "codebase-explorer" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'codebase-explorer' to be a built-in agent")
	}
}

func TestDispatcher_Dispatch_KnownAgent(t *testing.T) {
	prov := &mockProvider{response: endTurnResponse("## Summary\nFound the bug.")}
	d := newTestDispatcher(t, prov, nil)

	result, err := d.Dispatch(context.Background(), "codebase-explorer", "find all usages of Foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "## Summary\nFound the bug." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestDispatcher_Dispatch_UnknownAgent(t *testing.T) {
	prov := &mockProvider{response: endTurnResponse("ok")}
	d := newTestDispatcher(t, prov, nil)

	_, err := d.Dispatch(context.Background(), "nonexistent-agent", "do stuff")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestDispatcher_Dispatch_RestrictsTools(t *testing.T) {
	prov := &mockProvider{response: endTurnResponse("done")}
	d := newTestDispatcher(t, prov, nil)

	if _, err := d.Dispatch(context.Background(), "codebase-explorer", "task"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, def := range prov.lastDefs {
		if def.Name == "bash" || def.Name == "write_file" || def.Name == "dispatch_agent" {
			t.Errorf("unexpected tool %q passed to sub-agent", def.Name)
		}
	}
	found := false
	for _, def := range prov.lastDefs {
		if def.Name == "read_file" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'read_file' in codebase-explorer tool subset")
	}
}

func TestDispatcher_UserDefOverridesBuiltin(t *testing.T) {
	custom := &subagent.Definition{
		Name:         "codebase-explorer",
		Description:  "custom version",
		ToolSubset:   []string{"read_file"},
		SystemPrompt: "Custom prompt.",
	}
	prov := &mockProvider{response: endTurnResponse("ok")}
	d := newTestDispatcher(t, prov, []*subagent.Definition{custom})

	if _, err := d.Dispatch(context.Background(), "codebase-explorer", "task"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcher_Dispatch_ProviderError(t *testing.T) {
	errProv := &mockProvider{err: fmt.Errorf("provider offline")}
	d := newTestDispatcher(t, errProv, nil)

	_, err := d.Dispatch(context.Background(), "codebase-explorer", "task")
	if err == nil {
		t.Fatal("expected error when provider fails")
	}
}
