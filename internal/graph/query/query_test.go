package query_test

import (
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/query"
)

func buildGraph() *query.Graph {
	nodes := []graph.Node{
		{ID: "a/A.java::A", Kind: graph.KindClass, Name: "A", Path: "a/A.java"},
		{ID: "a/A.java::A.run", Kind: graph.KindFunction, Name: "A.run", Path: "a/A.java"},
		{ID: "b/B.java::B", Kind: graph.KindClass, Name: "B", Path: "b/B.java"},
		{ID: "b/B.java::B.handle", Kind: graph.KindFunction, Name: "B.handle", Path: "b/B.java"},
		{ID: "c/C.java::C", Kind: graph.KindClass, Name: "C", Path: "c/C.java"},
		{ID: "t/ATest.java::ATest", Kind: graph.KindTest, Name: "ATest", Path: "t/ATest.java"},
	}
	edges := []graph.Edge{
		{From: "a/A.java::A", To: "a/A.java::A.run", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "b/B.java::B", To: "b/B.java::B.handle", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "b/B.java::B", To: "a/A.java::A", Kind: graph.EdgeExtends, Confidence: graph.ConfExact},
		{From: "c/C.java::C", To: "b/B.java::B", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "a/A.java::A", To: "t/ATest.java::ATest", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
	}
	return query.NewGraph(nodes, edges)
}

func TestImpact(t *testing.T) {
	g := buildGraph()
	result, err := g.Impact("a/A.java::A", 2, query.DirUp, 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}

	// Direct callers/extends/implements of A should be found.
	if !result.Contains("b/B.java::B") {
		t.Errorf("expected B in impact of A; got %+v", result.NodeIDs())
	}
	// Depth 2: C calls B which extends A.
	if !result.Contains("c/C.java::C") {
		t.Errorf("expected C in depth-2 impact of A; got %+v", result.NodeIDs())
	}
	// tested_by should NOT be traversed in impact (test nodes aren't "callers").
	if result.Contains("t/ATest.java::ATest") {
		t.Errorf("test node should not appear in impact; got %+v", result.NodeIDs())
	}
	// Source node itself should not be in result.
	if result.Contains("a/A.java::A") {
		t.Errorf("source node should not be in impact result")
	}
}

func TestImpactDependentsOnlyNoMembers(t *testing.T) {
	g := buildFatGraph()
	res, err := g.Impact("u/U.java::U", 1, query.DirUp, 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(res.Hits) != 3 {
		t.Errorf("hits = %d, want 3 dependents (no members); got %+v", len(res.Hits), res.NodeIDs())
	}
	for _, h := range res.Hits {
		if h.Via == graph.EdgeContains {
			t.Errorf("found a contains (member) hit; members must not be expanded: %s", h.Node.ID)
		}
		if h.Subsystem == "" {
			t.Errorf("hit %s has empty Subsystem", h.Node.ID)
		}
	}
}

func TestImpactSafetyCap(t *testing.T) {
	g := buildFatGraph()
	res, err := g.Impact("u/U.java::U", 1, query.DirUp, 2)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(res.Hits) != 2 || !res.Truncated {
		t.Errorf("safety cap not honored: hits=%d truncated=%v", len(res.Hits), res.Truncated)
	}
}

func TestFormatImpactAggregatesTail(t *testing.T) {
	g := buildFatGraph()
	res, _ := g.Impact("u/U.java::U", 1, query.DirUp, 0)
	out := query.FormatImpact("u/U.java::U", res, 2)
	if !strings.Contains(out, "3 dependent") {
		t.Errorf("header missing true total: %s", out)
	}
	if strings.Count(out, "**d/") != 2 {
		t.Errorf("want exactly 2 dependent file headers (display cap), got %d: %s", strings.Count(out, "**d/"), out)
	}
	if !strings.Contains(out, "of 3") || !strings.Contains(out, "more") {
		t.Errorf("missing 'showing X of 3 … more' note: %s", out)
	}
	if !strings.Contains(out, "By subsystem") {
		t.Errorf("missing subsystem breakdown: %s", out)
	}
}

func TestImpactBridgesToContainingFile(t *testing.T) {
	// File-level import edges are the backbone of Java dependents. A query for a
	// class symbol must reach the reverse import edges of its containing file,
	// even though those edges are keyed by the file node, not the class node.
	nodes := []graph.Node{
		{ID: "m/Svc.java", Kind: graph.KindFile, Name: "m/Svc.java", Path: "m/Svc.java"},
		{ID: "m/Svc.java::Svc", Kind: graph.KindClass, Name: "Svc", Path: "m/Svc.java"},
		{ID: "w/Ctrl.java", Kind: graph.KindFile, Name: "w/Ctrl.java", Path: "w/Ctrl.java"},
	}
	edges := []graph.Edge{
		{From: "m/Svc.java", To: "m/Svc.java::Svc", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "w/Ctrl.java", To: "m/Svc.java", Kind: graph.EdgeImports, Confidence: graph.ConfExact},
	}
	g := query.NewGraph(nodes, edges)

	result, err := g.Impact("m/Svc.java::Svc", 2, query.DirUp, 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if !result.Contains("w/Ctrl.java") {
		t.Errorf("expected w/Ctrl.java (file-level importer) in impact of class Svc; got %+v", result.NodeIDs())
	}
}

func TestFind(t *testing.T) {
	g := buildGraph()
	matches := g.Find("B")
	if len(matches) == 0 {
		t.Fatal("expected at least one match for 'B'")
	}
	var found bool
	for _, n := range matches {
		if n.ID == "b/B.java::B" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected b/B.java::B in Find('B'); got %+v", matches)
	}
}

func TestFindCaseInsensitive(t *testing.T) {
	g := buildGraph()
	lower := g.Find("a")
	upper := g.Find("A")
	if len(lower) == 0 || len(upper) == 0 {
		t.Fatal("expected matches for both 'a' and 'A'")
	}
	// Both should return the same count.
	if len(lower) != len(upper) {
		t.Errorf("case-insensitive: Find('a') returned %d, Find('A') returned %d", len(lower), len(upper))
	}
}

func TestFormatImpact(t *testing.T) {
	g := buildGraph()
	result, err := g.Impact("a/A.java::A", 2, query.DirUp, 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	out := query.FormatImpact("a/A.java::A", result, 0)
	// Must be non-empty markdown.
	if len(out) == 0 {
		t.Fatal("FormatImpact returned empty string")
	}
	// Must include file paths.
	if !strings.Contains(out, "b/B.java") {
		t.Errorf("expected b/B.java in formatted output; got:\n%s", out)
	}
	// Must NOT include full source code (pointer-only output).
	if strings.Count(out, "\n") > 50 {
		t.Errorf("FormatImpact output is too long (%d lines); should be pointer-only", strings.Count(out, "\n"))
	}
}

func TestNodeByID(t *testing.T) {
	g := buildGraph()
	n, ok := g.NodeByID("a/A.java::A")
	if !ok {
		t.Fatal("NodeByID: expected to find a/A.java::A")
	}
	if n.Name != "A" {
		t.Errorf("NodeByID: got name %q, want A", n.Name)
	}
	_, ok2 := g.NodeByID("nonexistent::X")
	if ok2 {
		t.Error("NodeByID: expected false for nonexistent id")
	}
}

func TestFormatFind(t *testing.T) {
	g := buildGraph()
	// Non-empty results.
	nodes := g.Find("B")
	out := query.FormatFind("B", nodes)
	if !strings.Contains(out, "## Symbols matching") {
		t.Errorf("FormatFind: missing header; got:\n%s", out)
	}
	if !strings.Contains(out, "B.java") {
		t.Errorf("FormatFind: expected B.java in output; got:\n%s", out)
	}
	// Empty results.
	empty := query.FormatFind("zzznomatch", nil)
	if !strings.Contains(empty, "No symbols match") {
		t.Errorf("FormatFind empty: expected 'No symbols match'; got:\n%s", empty)
	}
}

func TestFormatFindWithLine(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a/A.java::A", Kind: graph.KindClass, Name: "A", Path: "a/A.java", Line: 3},
		{ID: "b/B.java::B", Kind: graph.KindClass, Name: "B", Path: "b/B.java", Line: 0},
	}
	out := query.FormatFind("query", nodes)
	// Node with line > 0 should include line number.
	if !strings.Contains(out, ":3") {
		t.Errorf("FormatFind: expected ':3' for node with line 3; got:\n%s", out)
	}
}

// synthetic provides a minimal node+edge set for knowledge-layer tests.
func synthetic() ([]graph.Node, []graph.Edge) {
	nodes := []graph.Node{
		{ID: "f1.java", Kind: graph.KindFile, Name: "f1.java", Path: "f1.java"},
		{ID: "f1.java::A", Kind: graph.KindClass, Name: "A", Path: "f1.java", Line: 1, EndLine: 5},
		{ID: "f2.java", Kind: graph.KindFile, Name: "f2.java", Path: "f2.java"},
		{ID: "f2.java::B", Kind: graph.KindClass, Name: "B", Path: "f2.java", Line: 1, EndLine: 5},
	}
	edges := []graph.Edge{
		{From: "f1.java", To: "f1.java::A", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "f2.java", To: "f2.java::B", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "f2.java::B", To: "f1.java::A", Kind: graph.EdgeExtends, Confidence: graph.ConfHigh},
	}
	return nodes, edges
}

// knowledgeWorld extends the synthetic graph with a skill documenting f1.java,
// a global convention node, and a tested_by edge for A.
func knowledgeWorld() ([]graph.Node, []graph.Edge) {
	nodes, edges := synthetic()
	nodes = append(nodes,
		graph.Node{ID: "skill::core", Kind: graph.KindSkill, Name: "core", Path: ".claude/skills/core/SKILL.md", Line: 1},
		graph.Node{ID: "convention::project", Kind: graph.KindConvention, Name: "conventions", Path: "CLAUDE.md", Line: 5, EndLine: 20},
		graph.Node{ID: "f1.java::ATest", Kind: graph.KindTest, Name: "ATest", Path: "f1test.java", Line: 1, EndLine: 4},
	)
	edges = append(edges,
		graph.Edge{From: "skill::core", To: "f1.java", Kind: graph.EdgeDocuments, Confidence: graph.ConfHigh},
		graph.Edge{From: "f1.java::A", To: "f1.java::ATest", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
	)
	return nodes, edges
}

func TestImpactIgnoresDocumentsEdges(t *testing.T) {
	nodes, edges := knowledgeWorld()
	g := query.NewGraph(nodes, edges)
	res, err := g.Impact("f1.java::A", 2, query.DirUp, 50)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, h := range res.Hits {
		if h.Node.Kind == graph.KindSkill {
			t.Errorf("skill node leaked into impact BFS: %+v", h)
		}
	}
}

func TestContextAssemblesEverything(t *testing.T) {
	nodes, edges := knowledgeWorld()
	g := query.NewGraph(nodes, edges)
	res, err := g.Context("f1.java::A", 2)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].ID != "skill::core" {
		t.Errorf("Skills = %+v, want skill::core (documents f1.java, A's file)", res.Skills)
	}
	if len(res.Conventions) != 1 || res.Conventions[0].ID != "convention::project" {
		t.Errorf("Conventions = %+v, want the global convention", res.Conventions)
	}
	if len(res.Tests) != 1 || res.Tests[0].ID != "f1.java::ATest" {
		t.Errorf("Tests = %+v, want f1.java::ATest", res.Tests)
	}
	out := query.FormatContext(res, 50)
	for _, want := range []string{"Blast radius", "Domain skills", "Conventions", "Tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatContext missing section %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "public class") {
		t.Errorf("context output must never contain source code")
	}
}

func TestCallersCalleesSkillsFor(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a.go::A", Kind: graph.KindFunction, Name: "A", Path: "a.go"},
		{ID: "b.go::B", Kind: graph.KindFunction, Name: "B", Path: "b.go"},
		{ID: "c.go::C", Kind: graph.KindFunction, Name: "C", Path: "c.go"},
		{ID: "skill::core", Kind: graph.KindSkill, Name: "core", Path: ".claude/skills/core/SKILL.md"},
	}
	edges := []graph.Edge{
		{From: "b.go::B", To: "a.go::A", Kind: graph.EdgeCalls},
		{From: "c.go::C", To: "a.go::A", Kind: graph.EdgeCalls},
		{From: "c.go::C", To: "a.go::A", Kind: graph.EdgeCalls}, // duplicate: must dedupe
		{From: "a.go::A", To: "b.go::B", Kind: graph.EdgeCalls},
		{From: "b.go::B", To: "a.go::A", Kind: graph.EdgeExtends}, // non-calls: must be ignored
		{From: "skill::core", To: "a.go", Kind: graph.EdgeDocuments},
	}
	g := query.NewGraph(nodes, edges)

	callers := g.Callers("a.go::A")
	if len(callers) != 2 || callers[0].ID != "b.go::B" || callers[1].ID != "c.go::C" {
		t.Fatalf("Callers = %+v, want [b.go::B c.go::C]", callers)
	}
	callees := g.Callees("a.go::A")
	if len(callees) != 1 || callees[0].ID != "b.go::B" {
		t.Fatalf("Callees = %+v, want [b.go::B]", callees)
	}
	skills := g.SkillsFor("a.go")
	if len(skills) != 1 || skills[0].ID != "skill::core" {
		t.Fatalf("SkillsFor = %+v, want [skill::core]", skills)
	}
	if got := g.SkillsFor("zzz.go"); len(got) != 0 {
		t.Fatalf("SkillsFor(unknown) = %+v, want empty", got)
	}
}

func TestGraph_CyclicCoreOf(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a", Name: "a", Path: "a.go"},
		{ID: "b", Name: "b", Path: "b.go"},
		{ID: "c", Name: "c", Path: "c.go"},
	}
	edges := []graph.Edge{
		{From: "a", To: "b", Kind: graph.EdgeCalls},
		{From: "b", To: "a", Kind: graph.EdgeCalls}, // a<->b cycle
		{From: "c", To: "a", Kind: graph.EdgeCalls}, // c depends on the cycle, not in it
	}
	g := query.NewGraph(nodes, edges)

	core, ok := g.CyclicCoreOf("a")
	if !ok || len(core) != 2 || core[0] != "a" || core[1] != "b" {
		t.Errorf("CyclicCoreOf(a) = %v, %v; want [a b], true", core, ok)
	}
	if _, ok := g.CyclicCoreOf("c"); ok {
		t.Errorf("c is not in a cycle; want ok=false")
	}
}

func TestFormatContext_ShowsCyclicCore(t *testing.T) {
	out := query.FormatContext(&query.ContextResult{
		Target:         "a",
		CyclicCore:     []string{"a", "b"},
		CyclicCoreSize: 2,
		Impact:         &query.ImpactResult{},
	}, 50)
	if !strings.Contains(out, "Cyclic core") || !strings.Contains(out, "group of 2") {
		t.Errorf("FormatContext should show the cyclic-core warning; got:\n%s", out)
	}
}

func TestContextFlagsChokepoint(t *testing.T) {
	nodes := []graph.Node{{ID: "H", Kind: graph.KindFunction, Name: "H", Path: "h.go"}}
	var edges []graph.Edge
	for _, leaf := range []string{"L1", "L2", "L3", "L4"} {
		nodes = append(nodes, graph.Node{ID: leaf, Kind: graph.KindFunction, Name: leaf, Path: leaf + ".go"})
		edges = append(edges, graph.Edge{From: leaf, To: "H", Kind: graph.EdgeCalls}, graph.Edge{From: "H", To: leaf, Kind: graph.EdgeCalls})
	}
	g := query.NewGraph(nodes, edges)
	res, err := g.Context("H", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsChokepoint || res.BridgeScore <= 0 {
		t.Errorf("context for hub should flag chokepoint: %+v", res)
	}
	if !strings.Contains(query.FormatContext(res, 50), "Chokepoint (bridge node)") {
		t.Errorf("FormatContext should print the chokepoint warning:\n%s", query.FormatContext(res, 50))
	}
}

// buildFatGraph models a utility U imported by 3 files, each containing a class
// with 3 methods. Reverse impact of U should yield 3 *dependents*, but naive
// node counting would see 3 files x (1 class + 3 methods) = 12 contains-expanded
// nodes and let a small cap hide most dependents.
func buildFatGraph() *query.Graph {
	nodes := []graph.Node{
		{ID: "u/U.java", Kind: graph.KindFile, Name: "u/U.java", Path: "u/U.java"},
		{ID: "u/U.java::U", Kind: graph.KindClass, Name: "U", Path: "u/U.java"},
	}
	var edges []graph.Edge
	edges = append(edges, graph.Edge{From: "u/U.java", To: "u/U.java::U", Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	for _, d := range []string{"D1", "D2", "D3"} {
		file := "d/" + d + ".java"
		class := file + "::" + d
		nodes = append(nodes,
			graph.Node{ID: file, Kind: graph.KindFile, Name: file, Path: file},
			graph.Node{ID: class, Kind: graph.KindClass, Name: d, Path: file},
		)
		edges = append(edges,
			graph.Edge{From: file, To: class, Kind: graph.EdgeContains, Confidence: graph.ConfExact},
			graph.Edge{From: file, To: "u/U.java", Kind: graph.EdgeImports, Confidence: graph.ConfExact},
		)
		for _, m := range []string{"m1", "m2", "m3"} {
			meth := file + "::" + d + "." + m
			nodes = append(nodes, graph.Node{ID: meth, Kind: graph.KindFunction, Name: d + "." + m, Path: file})
			edges = append(edges, graph.Edge{From: class, To: meth, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		}
	}
	return query.NewGraph(nodes, edges)
}

func TestFormatImpactRendersAllReturnedDependents(t *testing.T) {
	g := buildFatGraph()
	// Impact with safety cap=2 returns 2 dependents (no members) and Truncated=true.
	// FormatImpact with displayCap=2 shows both files and the aggregation tail.
	res, err := g.Impact("u/U.java::U", 1, query.DirUp, 2)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	out := query.FormatImpact("u/U.java::U", res, 2)

	// Both returned dependent files must appear as headers.
	if got := len(res.Hits); got != 2 {
		t.Fatalf("precondition: hits = %d, want 2", got)
	}
	headers := strings.Count(out, "**d/")
	if headers != 2 {
		t.Errorf("rendered dependent file headers = %d, want 2; output:\n%s", headers, out)
	}

	// Header must mention dependent(s).
	if !strings.Contains(out, "dependent") {
		t.Errorf("expected 'dependent' in header, got:\n%s", out)
	}
	// By subsystem section must be present.
	if !strings.Contains(out, "By subsystem") {
		t.Errorf("missing subsystem breakdown:\n%s", out)
	}
	// Traversal cap note when result is truncated.
	if !strings.Contains(out, "traversal capped") {
		t.Errorf("expected traversal-capped note when Impact Truncated=true:\n%s", out)
	}
}

func TestFormatImpactNoTruncationNoticeWhenComplete(t *testing.T) {
	g := buildFatGraph()
	res, err := g.Impact("u/U.java::U", 1, query.DirUp, 0) // unlimited
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	out := query.FormatImpact("u/U.java::U", res, 0)
	// No traversal-cap note when not truncated.
	if strings.Contains(out, "traversal capped") {
		t.Errorf("unexpected traversal-cap notice when not truncated:\n%s", out)
	}
	// No display-cap note when displayCap=0 (show all).
	if strings.Contains(out, "showing") && strings.Contains(out, "more") {
		t.Errorf("unexpected display-cap note when displayCap=0:\n%s", out)
	}
	// All 3 dependent files shown.
	if strings.Count(out, "**d/") != 3 {
		t.Errorf("expected 3 dependent file headers, got:\n%s", out)
	}
	// By subsystem section present.
	if !strings.Contains(out, "By subsystem") {
		t.Errorf("missing subsystem breakdown:\n%s", out)
	}
}

func TestImpactCapsOnDependentsNotNodes(t *testing.T) {
	g := buildFatGraph()

	// Cap of 2 must yield 2 *dependents* (not 2 nodes), and flag truncation.
	res, err := g.Impact("u/U.java::U", 1, query.DirUp, 2)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if got := len(res.Hits); got != 2 {
		t.Errorf("dependents with cap=2 = %d, want 2; hits=%+v", got, res.NodeIDs())
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true when cap is reached")
	}

	// Cap >= dependent count: all 3 dependents, no truncation.
	full, err := g.Impact("u/U.java::U", 1, query.DirUp, 3)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if got := len(full.Hits); got != 3 {
		t.Errorf("dependents with cap=3 = %d, want 3", got)
	}
	if full.Truncated {
		t.Errorf("Truncated = true, want false when cap not reached")
	}

	// Unlimited (0) also returns all 3 dependents.
	unl, err := g.Impact("u/U.java::U", 1, query.DirUp, 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if got := len(unl.Hits); got != 3 {
		t.Errorf("dependents with cap=0 = %d, want 3", got)
	}
}

func TestSubsystemOf(t *testing.T) {
	pkgs := map[string]string{"a/b/Widget.java": "com.acme.widget"}
	g := query.NewGraph(nil, nil, query.WithPackages(pkgs))

	if got := query.SubsystemOf(g, graph.Node{Path: "a/b/Widget.java"}); got != "com.acme.widget" {
		t.Errorf("package case = %q, want com.acme.widget", got)
	}
	if got := query.SubsystemOf(g, graph.Node{Path: "x/y/z/Thing.go"}); got != "x/y/z" {
		t.Errorf("fallback case = %q, want x/y/z", got)
	}
	if got := query.SubsystemOf(g, graph.Node{Path: "Top.go"}); got != "(root)" {
		t.Errorf("root case = %q, want (root)", got)
	}
}

func TestFormatContextDependentsOnlyAggregated(t *testing.T) {
	g := buildFatGraph()
	res, err := g.Context("u/U.java::U", 1)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	out := query.FormatContext(res, 2)
	if !strings.Contains(out, "Blast radius") {
		t.Errorf("missing blast radius section: %s", out)
	}
	if !strings.Contains(out, "By subsystem") {
		t.Errorf("missing subsystem breakdown: %s", out)
	}
	if strings.Contains(out, ".m1") || strings.Contains(out, ".m2") {
		t.Errorf("member node leaked into context output: %s", out)
	}
	// Display cap of 2 over 3 dependents: exactly 2 listed + the truncation note.
	if !strings.Contains(out, "showing 2 of 3") {
		t.Errorf("missing truncation note (showing 2 of 3): %s", out)
	}
	if shown := strings.Count(out, " via "); shown != 2 {
		t.Errorf("expected 2 shown dependents, got %d: %s", shown, out)
	}
}
