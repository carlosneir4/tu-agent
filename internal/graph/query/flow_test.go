package query

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

// flowFixture models a fictional message-processing pipeline:
//
//	pipeline/Receiver.java::Receiver.receive
//	  → (calls, boundary) transform/Parser.java::Parser.parse
//	      → (calls, boundary) publish/Publisher.java::Publisher.publish  [interface]
//	          dispatch candidate: publish/Emitter.java::Emitter.publish
//	      → (calls, visited) pipeline/...::Receiver.receive  [cycle, skipped]
//	  → (calls, same pkg) pipeline/Receiver.java::Receiver.validate
func flowFixture() *Graph {
	nodes := []graph.Node{
		{ID: "pipeline/Receiver.java::Receiver", Kind: graph.KindClass, Name: "Receiver", Path: "pipeline/Receiver.java", Line: 3},
		{ID: "pipeline/Receiver.java::Receiver.receive", Kind: graph.KindFunction, Name: "Receiver.receive", Path: "pipeline/Receiver.java", Line: 5},
		{ID: "pipeline/Receiver.java::Receiver.validate", Kind: graph.KindFunction, Name: "Receiver.validate", Path: "pipeline/Receiver.java", Line: 10},
		{ID: "transform/Parser.java::Parser", Kind: graph.KindClass, Name: "Parser", Path: "transform/Parser.java", Line: 3},
		{ID: "transform/Parser.java::Parser.parse", Kind: graph.KindFunction, Name: "Parser.parse", Path: "transform/Parser.java", Line: 5},
		{ID: "publish/Publisher.java::Publisher", Kind: graph.KindClass, Name: "Publisher", Path: "publish/Publisher.java", Line: 3},
		{ID: "publish/Publisher.java::Publisher.publish", Kind: graph.KindFunction, Name: "Publisher.publish", Path: "publish/Publisher.java", Line: 4},
		{ID: "publish/Emitter.java::Emitter", Kind: graph.KindClass, Name: "Emitter", Path: "publish/Emitter.java", Line: 3},
		{ID: "publish/Emitter.java::Emitter.publish", Kind: graph.KindFunction, Name: "Emitter.publish", Path: "publish/Emitter.java", Line: 4},
	}
	edges := []graph.Edge{
		// contains
		{From: "pipeline/Receiver.java::Receiver", To: "pipeline/Receiver.java::Receiver.receive", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "pipeline/Receiver.java::Receiver", To: "pipeline/Receiver.java::Receiver.validate", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "transform/Parser.java::Parser", To: "transform/Parser.java::Parser.parse", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "publish/Publisher.java::Publisher", To: "publish/Publisher.java::Publisher.publish", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "publish/Emitter.java::Emitter", To: "publish/Emitter.java::Emitter.publish", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		// implements
		{From: "publish/Emitter.java::Emitter", To: "publish/Publisher.java::Publisher", Kind: graph.EdgeImplements, Confidence: graph.ConfExact},
		// calls (including back-edge to test cycle safety)
		{From: "pipeline/Receiver.java::Receiver.receive", To: "transform/Parser.java::Parser.parse", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "pipeline/Receiver.java::Receiver.receive", To: "pipeline/Receiver.java::Receiver.validate", Kind: graph.EdgeCalls, Confidence: graph.ConfExact},
		{From: "transform/Parser.java::Parser.parse", To: "publish/Publisher.java::Publisher.publish", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "transform/Parser.java::Parser.parse", To: "pipeline/Receiver.java::Receiver.receive", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh}, // back-edge
	}
	return NewGraph(nodes, edges)
}

func TestGraph_Flow_MultiHop(t *testing.T) {
	g := flowFixture()
	res, err := g.Flow("pipeline/Receiver.java::Receiver.receive", 5, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	if res.Entry.Name != "Receiver.receive" {
		t.Errorf("Entry = %q, want Receiver.receive", res.Entry.Name)
	}
	if len(res.Callees) != 2 {
		t.Fatalf("top-level callees = %d, want 2 (validate + parse): %+v", len(res.Callees), res.Callees)
	}
	// Callees are sorted by node ID: pipeline/...validate < transform/...parse
	validate := res.Callees[0]
	parse := res.Callees[1]
	if validate.Node.Name != "Receiver.validate" {
		t.Errorf("Callees[0] = %q, want Receiver.validate", validate.Node.Name)
	}
	if validate.CrossesBoundary {
		t.Errorf("Receiver.validate CrossesBoundary = true, want false (same package)")
	}
	if len(validate.Callees) != 0 {
		t.Errorf("Receiver.validate callees = %d, want 0", len(validate.Callees))
	}
	if parse.Node.Name != "Parser.parse" {
		t.Errorf("Callees[1] = %q, want Parser.parse", parse.Node.Name)
	}
	if !parse.CrossesBoundary {
		t.Errorf("Parser.parse CrossesBoundary = false, want true (pipeline/ → transform/)")
	}
	// Parser.parse's only unvisited callee is Publisher.publish (interface).
	if len(parse.Callees) != 1 {
		t.Fatalf("Parser.parse callees = %d, want 1 (Publisher.publish): %+v", len(parse.Callees), parse.Callees)
	}
	pub := parse.Callees[0]
	if pub.Node.Name != "Publisher.publish" {
		t.Errorf("Parser.parse.Callees[0] = %q, want Publisher.publish", pub.Node.Name)
	}
	if len(pub.DispatchCandidates) != 1 || pub.DispatchCandidates[0].Name != "Emitter.publish" {
		t.Errorf("DispatchCandidates = %+v, want [Emitter.publish]", pub.DispatchCandidates)
	}
	if len(pub.Callees) != 0 {
		t.Errorf("interface hop Callees = %d, want 0 (dispatch stops recursion)", len(pub.Callees))
	}
}

func TestGraph_Flow_DepthLimit(t *testing.T) {
	g := flowFixture()
	res, err := g.Flow("pipeline/Receiver.java::Receiver.receive", 1, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	if len(res.Callees) != 2 {
		t.Fatalf("callees = %d, want 2", len(res.Callees))
	}
	for _, h := range res.Callees {
		if len(h.Callees) != 0 {
			t.Errorf("%s has sub-callees, depth=1 should stop after first hop", h.Node.Name)
		}
	}
}

func TestGraph_Flow_CycleSafe(t *testing.T) {
	g := flowFixture()
	// depth=10 would loop forever if cycle detection is broken
	res, err := g.Flow("pipeline/Receiver.java::Receiver.receive", 10, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	// Receiver.receive must not appear as a callee of Parser.parse
	for _, h := range res.Callees {
		if h.Node.Name == "Parser.parse" {
			for _, sub := range h.Callees {
				if sub.Node.Name == "Receiver.receive" {
					t.Errorf("cycle not broken: Receiver.receive reappears under Parser.parse")
				}
			}
		}
	}
}

func TestGraph_Flow_FanOutCap(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a.go::Root", Kind: graph.KindFunction, Name: "Root", Path: "a.go"},
		{ID: "a.go::B", Kind: graph.KindFunction, Name: "B", Path: "a.go"},
		{ID: "a.go::C", Kind: graph.KindFunction, Name: "C", Path: "a.go"},
		{ID: "a.go::D", Kind: graph.KindFunction, Name: "D", Path: "a.go"},
	}
	edges := []graph.Edge{
		{From: "a.go::Root", To: "a.go::B", Kind: graph.EdgeCalls, Confidence: graph.ConfExact},
		{From: "a.go::Root", To: "a.go::C", Kind: graph.EdgeCalls, Confidence: graph.ConfExact},
		{From: "a.go::Root", To: "a.go::D", Kind: graph.EdgeCalls, Confidence: graph.ConfExact},
	}
	g := NewGraph(nodes, edges)
	res, err := g.Flow("a.go::Root", 5, 2) // cap at 2 of 3 callees
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	if len(res.Callees) != 2 {
		t.Errorf("callees = %d, want 2 (fan-out cap)", len(res.Callees))
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

func TestGraph_Flow_UnknownTarget(t *testing.T) {
	g := flowFixture()
	if _, err := g.Flow("NoSuchNode", 5, 10); err == nil {
		t.Fatal("Flow on unknown node should error")
	}
}

func TestFormatFlow(t *testing.T) {
	g := flowFixture()
	res, err := g.Flow("pipeline/Receiver.java::Receiver.receive", 5, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	out := FormatFlow(res)
	for _, want := range []string{
		"Execution flow from `Receiver.receive`",
		"Receiver.validate",
		"pipeline/Receiver.java:10",
		"Parser.parse",
		"[boundary]",
		"Publisher.publish",
		"[boundary, interface dispatch]",
		"? Emitter.publish",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatFlow missing %q in:\n%s", want, out)
		}
	}
}

func TestFormatFlow_NoCalls(t *testing.T) {
	g := flowFixture()
	res, err := g.Flow("pipeline/Receiver.java::Receiver.validate", 5, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	if out := FormatFlow(res); !strings.Contains(out, "No outgoing calls found") {
		t.Errorf("FormatFlow = %q, want the empty-result line", out)
	}
}

func TestFormatFlowMermaid(t *testing.T) {
	g := flowFixture()
	res, err := g.Flow("pipeline/Receiver.java::Receiver.receive", 5, 10)
	if err != nil {
		t.Fatalf("Flow: %v", err)
	}
	out := FormatFlowMermaid(res)
	for _, want := range []string{
		"flowchart LR",
		"N0",
		"Receiver.receive",
		"Parser.parse",
		"boundary",
		"dispatch",
		"Emitter.publish",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatFlowMermaid missing %q in:\n%s", want, out)
		}
	}
	// determinism
	out2 := FormatFlowMermaid(res)
	if out != out2 {
		t.Errorf("FormatFlowMermaid is not deterministic")
	}
}
