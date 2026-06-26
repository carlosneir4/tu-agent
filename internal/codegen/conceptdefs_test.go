package codegen

import (
	"context"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/provider"
)

func TestIsDeterministicDescription(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"fallback shape", "internal/memory — 10 files; landmarks: store.go, fts.go", true},
		{"fallback no landmark names", "scripts — 3 files; landmarks: ", true},
		{"llm description", "The persistent memory store — SQLite-backed durable notes.", false},
		{"hand-written", "Run the project's tests for a package.", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDeterministicDescription(tt.desc); got != tt.want {
				t.Errorf("IsDeterministicDescription(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestPreservedDefinition(t *testing.T) {
	tests := []struct {
		name     string
		cardDef  string
		existing string
		want     string
	}{
		{"fresh definition wins", "Freshly generated.", "Old LLM desc.", "Freshly generated."},
		{"fresh wins over no existing", "Freshly generated.", "", "Freshly generated."},
		{"preserve existing llm when card empty", "", "The persistent memory store — durable notes.", "The persistent memory store — durable notes."},
		{"overwrite existing fallback", "", "internal/memory — 10 files; landmarks: store.go", ""},
		{"no existing, no card def", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PreservedDefinition(tt.cardDef, tt.existing); got != tt.want {
				t.Errorf("PreservedDefinition(%q, %q) = %q, want %q", tt.cardDef, tt.existing, got, tt.want)
			}
		})
	}
}

// mockTextProvider returns a fixed response for any Send call.
type mockTextProvider struct{ response string }

func newMockProvider(response string) *mockTextProvider { return &mockTextProvider{response: response} }

func (m *mockTextProvider) Send(_ context.Context, _ string, _ []provider.Message, _ []provider.ToolDef) (provider.Response, error) {
	return provider.Response{
		Blocks:     []provider.Block{{Type: "text", Text: m.response}},
		StopReason: "end_turn",
	}, nil
}
func (m *mockTextProvider) Name() string             { return "mock" }
func (m *mockTextProvider) Model() string            { return "mock-model" }
func (m *mockTextProvider) NativeContextWindow() int { return 200000 }

func TestGenerateConceptDefinitionsParsesAndApplies(t *testing.T) {
	cards := []ConceptCard{
		{Concept: Concept{Name: "catalog", Package: "p.catalog"}},
		{Concept: Concept{Name: "orders", Package: "p.orders"}},
	}
	mock := newMockProvider("catalog: Product browsing and pricing.\norders: Order lifecycle from cart to fulfillment.\n")

	defs, err := GenerateConceptDefinitions(context.Background(), cards, mock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if defs["catalog"] != "Product browsing and pricing." {
		t.Errorf("catalog def = %q", defs["catalog"])
	}
	ApplyDefinitions(cards, defs)
	if cards[1].Definition != "Order lifecycle from cart to fulfillment." {
		t.Errorf("orders definition not applied: %q", cards[1].Definition)
	}
}

func TestGenerateConceptDefinitionsToleratesPartialOutput(t *testing.T) {
	cards := []ConceptCard{{Concept: Concept{Name: "catalog"}}, {Concept: Concept{Name: "orders"}}}
	mock := newMockProvider("catalog: Only this one.\nnoise line without colon\nunknown: ignored\n")
	defs, err := GenerateConceptDefinitions(context.Background(), cards, mock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs["catalog"] != "Only this one." {
		t.Errorf("defs = %v, want only catalog", defs)
	}
}

func TestSetCardDescription(t *testing.T) {
	content := "---\nname: widgets\ndescription: acme.widgets — 12 files; landmarks: Widget\n---\n\n# widgets (acme.widgets)\n\nLandmarks (by fan-in):\n- Widget\n"
	out, err := SetCardDescription(content, "Reusable UI building blocks composed into pages.")
	if err != nil {
		t.Fatalf("SetCardDescription: %v", err)
	}
	sk, err := ParseSkillContent(out)
	if err != nil {
		t.Fatalf("ParseSkillContent(out): %v", err)
	}
	if sk.Name != "widgets" {
		t.Errorf("name not preserved: %q", sk.Name)
	}
	if sk.Description != "Reusable UI building blocks composed into pages." {
		t.Errorf("description not set: %q", sk.Description)
	}
	if !strings.Contains(sk.Body, "Landmarks (by fan-in):") || !strings.Contains(sk.Body, "- Widget") {
		t.Errorf("body not preserved verbatim: %q", sk.Body)
	}
	if IsDeterministicDescription(sk.Description) {
		t.Errorf("description still looks like the deterministic fallback")
	}

	// A description containing YAML-hostile chars must be quoted and round-trip.
	out2, err := SetCardDescription(content, "Builds the URL: slug + section.")
	if err != nil {
		t.Fatalf("SetCardDescription quoted: %v", err)
	}
	sk2, err := ParseSkillContent(out2)
	if err != nil {
		t.Fatalf("ParseSkillContent(out2): %v", err)
	}
	if sk2.Description != "Builds the URL: slug + section." {
		t.Errorf("quoted description did not round-trip: %q", sk2.Description)
	}

	// No frontmatter → error.
	if _, err := SetCardDescription("no frontmatter", "x"); err == nil {
		t.Errorf("expected error on missing frontmatter")
	}
}
