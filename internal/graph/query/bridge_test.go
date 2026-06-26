package query

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

// callEdge is a calls edge between two function nodes.
func callEdge(from, to string) graph.Edge {
	return graph.Edge{From: from, To: to, Kind: graph.EdgeCalls}
}

// lineGraph A->B->C: B is the sole bridge between A and C.
func lineGraph() *Graph {
	nodes := []graph.Node{
		{ID: "A", Kind: graph.KindFunction, Name: "A"},
		{ID: "B", Kind: graph.KindFunction, Name: "B"},
		{ID: "C", Kind: graph.KindFunction, Name: "C"},
	}
	edges := []graph.Edge{callEdge("A", "B"), callEdge("B", "C")}
	return NewGraph(nodes, edges)
}

func TestBridgeScoresBridgeWins(t *testing.T) {
	g := lineGraph()
	scores := g.BridgeScores(BridgeConfig{})
	if scores["B"] <= 0 {
		t.Fatalf("bridge B should have positive betweenness, got %v", scores["B"])
	}
	if scores["A"] != 0 || scores["C"] != 0 {
		t.Errorf("endpoints should have zero betweenness: A=%v C=%v", scores["A"], scores["C"])
	}
}

func TestBridgeScoresDeterministic(t *testing.T) {
	g := lineGraph()
	a := g.BridgeScores(BridgeConfig{})
	b := lineGraph().BridgeScores(BridgeConfig{})
	for _, id := range []string{"A", "B", "C"} {
		if a[id] != b[id] {
			t.Errorf("non-deterministic score for %s: %v vs %v", id, a[id], b[id])
		}
	}
}

func TestBridgeScoresOnlyCallEdges(t *testing.T) {
	// An imports edge must not create a path for betweenness.
	nodes := []graph.Node{
		{ID: "A", Kind: graph.KindFunction, Name: "A"},
		{ID: "B", Kind: graph.KindFunction, Name: "B"},
		{ID: "C", Kind: graph.KindFunction, Name: "C"},
	}
	edges := []graph.Edge{callEdge("A", "B"), {From: "B", To: "C", Kind: graph.EdgeImports}}
	g := NewGraph(nodes, edges)
	if s := g.BridgeScores(BridgeConfig{})["B"]; s != 0 {
		t.Errorf("imports edge must not contribute betweenness, B=%v", s)
	}
}

func TestIsChokepoint(t *testing.T) {
	// A star: hub H is on every shortest path between the 4 leaves.
	nodes := []graph.Node{{ID: "H", Kind: graph.KindFunction, Name: "H"}}
	var edges []graph.Edge
	for _, leaf := range []string{"L1", "L2", "L3", "L4"} {
		nodes = append(nodes, graph.Node{ID: leaf, Kind: graph.KindFunction, Name: leaf})
		edges = append(edges, callEdge(leaf, "H"), callEdge("H", leaf))
	}
	g := NewGraph(nodes, edges)
	if score, ok := g.IsChokepoint("H", BridgeConfig{}); !ok || score <= 0 {
		t.Errorf("hub H should be a chokepoint, score=%v ok=%v", score, ok)
	}
	if _, ok := g.IsChokepoint("L1", BridgeConfig{}); ok {
		t.Error("leaf L1 should not be a chokepoint")
	}
}

func TestBridgeTop(t *testing.T) {
	g := lineGraph()
	top := g.BridgeTop(BridgeConfig{}, 10)
	if len(top) == 0 || top[0].ID != "B" {
		t.Fatalf("BridgeTop should rank B first, got %+v", top)
	}
	if len(top) >= 2 && top[0].Score < top[1].Score {
		t.Error("BridgeTop must be sorted descending by score")
	}
}
