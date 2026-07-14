package query

import (
	"reflect"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

func TestTestStructure_sharedBaseAndGrouping(t *testing.T) {
	nodes := []graph.Node{
		{ID: "Foo", Kind: graph.KindClass, Name: "Foo", Path: "Foo.java", Language: "java"},
		{ID: "Foo.bar", Kind: graph.KindFunction, Name: "bar", Path: "Foo.java", Language: "java", Exported: true},
		{ID: "FooTest", Kind: graph.KindTest, Name: "FooTest", Path: "FooTest.java", Language: "java"},
		{ID: "FooCustomTest", Kind: graph.KindTest, Name: "FooCustomTest", Path: "FooCustomTest.java", Language: "java"},
		{ID: "FooTestBase", Kind: graph.KindClass, Name: "FooTestBase", Path: "FooTestBase.java", Language: "java"},
	}
	edges := []graph.Edge{
		{From: "Foo", To: "Foo.bar", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "Foo", To: "FooTest", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
		{From: "Foo", To: "FooCustomTest", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
		{From: "FooTest", To: "FooTestBase", Kind: graph.EdgeExtends, Confidence: graph.ConfHigh},
		{From: "FooCustomTest", To: "FooTestBase", Kind: graph.EdgeExtends, Confidence: graph.ConfHigh},
	}
	g := NewGraph(nodes, edges)

	// Querying by the method resolves up to its containing class.
	got := g.TestStructure("Foo.bar")
	want := TestStructureResult{
		SharedBase: "FooTestBase",
		Grouping:   "per-concern",
		Siblings:   []string{"FooCustomTest", "FooTest"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TestStructure = %+v, want %+v", got, want)
	}
}

func TestTestStructure_noTests(t *testing.T) {
	nodes := []graph.Node{
		{ID: "Bare", Kind: graph.KindClass, Name: "Bare", Path: "Bare.java", Language: "java"},
	}
	g := NewGraph(nodes, nil)
	got := g.TestStructure("Bare")
	if got.Grouping != "none" || got.SharedBase != "" || len(got.Siblings) != 0 {
		t.Fatalf("TestStructure(no tests) = %+v, want zero/none", got)
	}
}
