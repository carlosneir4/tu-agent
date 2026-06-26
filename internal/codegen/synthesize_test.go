package codegen_test

import (
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestBuildSynthesisMessage_CompactWithEdges(t *testing.T) {
	domains := []codegen.DomainFact{
		{Name: "widgets", Description: "renders widgets", KeyFiles: []string{"core/Widget.java"}},
		{Name: "render", Description: "rendering engine", KeyFiles: []string{"core/Renderer.java"}},
	}
	edges := []codegen.Edge{{From: "widgets", To: "render"}}
	msg := codegen.BuildSynthesisMessage("acme", domains, edges, 0)
	for _, want := range []string{"widgets", "renders widgets", "core/Widget.java", "widgets -> render"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n%s", want, msg)
		}
	}
	// Token-budget intent: must not embed full skill bodies (only facts).
	if strings.Contains(msg, "## Purpose") {
		t.Errorf("message should not embed full skill bodies")
	}
}

func TestBuildSynthesisMessage_IncludesCyclicCore(t *testing.T) {
	domains := []codegen.DomainFact{
		{Name: "core", Description: "core", KeyFiles: []string{"core.go"}},
		{Name: "api", Description: "api", KeyFiles: []string{"api.go"}},
		{Name: "leaf", Description: "leaf", KeyFiles: []string{"leaf.go"}},
	}
	edges := []codegen.Edge{
		{From: "core", To: "api"},
		{From: "api", To: "core"},  // core<->api cycle
		{From: "leaf", To: "core"}, // leaf depends on the core, not in it
	}
	msg := codegen.BuildSynthesisMessage("proj", domains, edges, 0)
	if !strings.Contains(msg, "Cyclic core") {
		t.Errorf("message should state the cyclic core; got:\n%s", msg)
	}
	if !strings.Contains(msg, "api") || !strings.Contains(msg, "core") {
		t.Errorf("cyclic core should name its members; got:\n%s", msg)
	}
}

func TestBuildSynthesisMessage_NoCycleNoCore(t *testing.T) {
	domains := []codegen.DomainFact{{Name: "a", Description: "a"}, {Name: "b", Description: "b"}}
	edges := []codegen.Edge{{From: "a", To: "b"}} // DAG, no cycle
	msg := codegen.BuildSynthesisMessage("proj", domains, edges, 0)
	if strings.Contains(msg, "Cyclic core") {
		t.Errorf("acyclic edges must not report a cyclic core; got:\n%s", msg)
	}
}

func TestBuildSynthesisPrompt_RendersCyclicCore(t *testing.T) {
	p := codegen.BuildSynthesisPrompt()
	if !strings.Contains(p, "cyclic core") && !strings.Contains(p, "Cyclic core") {
		t.Errorf("prompt must instruct rendering the cyclic core; got:\n%s", p)
	}
}

func TestBuildSynthesisPrompt_PointsChangeImpactAtGraphTools(t *testing.T) {
	prompt := codegen.BuildSynthesisPrompt()
	if !strings.Contains(prompt, "Change-Impact Queries") {
		t.Errorf("synthesis prompt must add a Change-Impact Queries section; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "tu-agent graph context") {
		t.Errorf("synthesis prompt must point change-impact queries at the graph tools; got:\n%s", prompt)
	}
	if strings.Contains(prompt, "dependency-graph.json") {
		t.Errorf("synthesis prompt must not reference dependency-graph.json anymore; got:\n%s", prompt)
	}
}
