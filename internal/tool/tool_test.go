package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tool"
)

type fakeToolA struct{}

func (f *fakeToolA) Name() string        { return "tool-a" }
func (f *fakeToolA) Description() string { return "Fake tool A" }
func (f *fakeToolA) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (f *fakeToolA) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "output-a", nil
}

type fakeToolB struct{}

func (f *fakeToolB) Name() string        { return "tool-b" }
func (f *fakeToolB) Description() string { return "Fake tool B" }
func (f *fakeToolB) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (f *fakeToolB) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "output-b", nil
}

// compile-time interface check
var _ tool.Tool = (*fakeToolA)(nil)
var _ tool.Tool = (*fakeToolB)(nil)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(&fakeToolA{})
	r.Register(&fakeToolB{})

	got, ok := r.Get("tool-a")
	if !ok {
		t.Fatal("Get(tool-a): not found")
	}
	if got.Name() != "tool-a" {
		t.Errorf("Name() = %q, want %q", got.Name(), "tool-a")
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := tool.NewRegistry()
	_, ok := r.Get("missing")
	if ok {
		t.Error("Get(missing) should return false")
	}
}

func TestRegistry_Defs(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(&fakeToolA{})
	r.Register(&fakeToolB{})

	defs := r.Defs()
	if len(defs) != 2 {
		t.Fatalf("len(Defs()) = %d, want 2", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["tool-a"] || !names["tool-b"] {
		t.Errorf("Defs() names = %v, want tool-a and tool-b", names)
	}
}

func TestRegistry_Defs_IncludesDescription(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(&fakeToolA{})

	defs := r.Defs()
	if defs[0].Description != "Fake tool A" {
		t.Errorf("Defs()[0].Description = %q, want %q", defs[0].Description, "Fake tool A")
	}
}
