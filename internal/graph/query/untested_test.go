package query

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

// fn builds an exported function node spanning lines [line, line+span-1].
func fn(id, path string, line, span int) graph.Node {
	name := id[strings.LastIndex(id, ":")+1:]
	return graph.Node{ID: id, Kind: graph.KindFunction, Name: name, Path: path,
		Line: line, EndLine: line + span - 1, Language: "go", Exported: true}
}

func gapFixture() *Graph {
	nodes := []graph.Node{
		{ID: "svc.go", Kind: graph.KindFile, Name: "svc.go", Path: "svc.go", Language: "go"},
		{ID: "app.go", Kind: graph.KindFile, Name: "app.go", Path: "app.go", Language: "go"},
		fn("svc.go::Pay", "svc.go", 10, 10),
		fn("svc.go::Refund", "svc.go", 30, 10),
		fn("svc.go::Edge4", "svc.go", 45, 4),
		fn("svc.go::tiny", "svc.go", 50, 3),
		{ID: "svc.go::hidden", Kind: graph.KindFunction, Name: "hidden", Path: "svc.go",
			Line: 60, EndLine: 70, Language: "go", Exported: false},
		fn("svc.go::Covered", "svc.go", 80, 10),
		fn("prod.go::Outer", "prod.go", 1, 10),
		fn("util.go::Deep", "util.go", 5, 10),
		fn("caller.go::A", "caller.go", 1, 10),
		fn("caller.go::B", "caller.go", 20, 10),
		{ID: "billing.java::Acct", Kind: graph.KindClass, Name: "Acct", Path: "billing.java",
			Line: 1, EndLine: 90, Language: "java"},
		fn("billing.java::Acct.Open", "billing.java", 10, 10),
		{ID: "svc_test.go::TestCovered", Kind: graph.KindTest, Name: "TestCovered",
			Path: "svc_test.go", Line: 1, EndLine: 20, Language: "go"},
		{ID: "billing_test.java::AcctTest", Kind: graph.KindTest, Name: "AcctTest",
			Path: "billing_test.java", Line: 1, EndLine: 50, Language: "java"},
		fn("helpers_test.go::Helper", "helpers_test.go", 1, 10),
	}
	edges := []graph.Edge{
		{From: "caller.go::A", To: "svc.go::Pay", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "caller.go::B", To: "svc.go::Pay", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "svc_test.go::TestCovered", To: "svc.go::Covered", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "svc_test.go::TestCovered", To: "prod.go::Outer", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "prod.go::Outer", To: "util.go::Deep", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "billing.java::Acct", To: "billing_test.java::AcctTest", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
		{From: "billing.java::Acct", To: "billing.java::Acct.Open", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "svc.go", To: "svc.go::Pay", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "app.go", To: "svc.go", Kind: graph.EdgeImports, Confidence: graph.ConfExact},
	}
	return NewGraph(nodes, edges)
}

func gapIDs(gaps []Gap) []string {
	out := make([]string, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, g.Node.ID)
	}
	return out
}

func TestUntestedGaps_semantics(t *testing.T) {
	g := gapFixture()
	gaps, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	got := map[string]bool{}
	for _, gp := range gaps {
		got[gp.Node.ID] = true
	}
	wantGap := []string{"svc.go::Pay", "svc.go::Refund",
		"util.go::Deep", "caller.go::A", "caller.go::B"}
	for _, id := range wantGap {
		if !got[id] {
			t.Errorf("expected %s in gaps, got %v", id, gapIDs(gaps))
		}
	}
	wantAbsent := map[string]string{
		"svc.go::Covered":         "called directly by a test",
		"prod.go::Outer":          "called directly by a test",
		"billing.java::Acct.Open": "containing class has tested_by",
		"svc.go::tiny":            "below MinLines (span 3 < 4)",
		"svc.go::Edge4":           "trivial accessor (span 4, 0 callees)",
		"svc.go::hidden":          "unexported",
		"helpers_test.go::Helper": "lives in a test file",
	}
	for id, why := range wantAbsent {
		if got[id] {
			t.Errorf("%s should be excluded (%s)", id, why)
		}
	}
}

func TestUntestedGaps_scoringAndOrder(t *testing.T) {
	g := gapFixture()
	gaps, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	if len(gaps) == 0 || gaps[0].Node.ID != "svc.go::Pay" {
		t.Fatalf("highest-risk gap = %v, want svc.go::Pay first", gapIDs(gaps))
	}
	first := gaps[0]
	if first.FanIn != 2 {
		t.Errorf("Pay FanIn = %d, want 2", first.FanIn)
	}
	wantScore := float64(first.FanIn+1) * float64(first.BlastRadius) * complexityFactor(first.Span)
	if first.Score != wantScore {
		t.Errorf("Pay Score = %v, want (fan_in+1)*blast*complexity = %v", first.Score, wantScore)
	}
	for _, gp := range gaps {
		if gp.Node.ID == "svc.go::Refund" && gp.FanIn != 0 {
			t.Errorf("Refund FanIn = %d, want 0", gp.FanIn)
		}
	}
	for i := 1; i < len(gaps); i++ {
		if gaps[i].Score > gaps[i-1].Score {
			t.Errorf("gaps not sorted by score desc at index %d", i)
		}
		if gaps[i].Score == gaps[i-1].Score && gaps[i].Node.ID < gaps[i-1].Node.ID {
			t.Errorf("tie at index %d not broken by ID asc", i)
		}
	}
}

func TestUntestedGaps_criticalityAndTop(t *testing.T) {
	g := gapFixture()
	crit := map[string]float64{"svc.go::Refund": 100}
	gaps, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2, Criticality: crit, Top: 1})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("Top=1 returned %d gaps", len(gaps))
	}
	if gaps[0].Node.ID != "svc.go::Refund" {
		t.Errorf("criticality boost ignored: first gap = %s, want svc.go::Refund", gaps[0].Node.ID)
	}
}

// TestUntestedGaps_excludesTrivialAccessors pins the complexity-aware filter:
// a short method that calls nothing (a getter/setter/builder passthrough) is
// excluded no matter how high its fan-in, while an equally short method that
// delegates to a callee (real logic) stays. This stops a fluent builder setter
// — reached by everything, so a huge fan-in×blast score — from topping the list.
func TestUntestedGaps_excludesTrivialAccessors(t *testing.T) {
	nodes := []graph.Node{
		{ID: "b.go", Kind: graph.KindFile, Name: "b.go", Path: "b.go", Language: "go"},
		fn("b.go::Setter", "b.go", 10, 4), // span 4, no callees → trivial
		fn("b.go::Logic", "b.go", 20, 4),  // span 4, but delegates → real logic
		fn("b.go::C1", "b.go", 40, 8),     // callers give Setter a high fan-in
		fn("b.go::C2", "b.go", 50, 8),
		fn("b.go::C3", "b.go", 60, 8),
		{ID: "external::dep", Kind: graph.KindExternal, Name: "dep", Path: "external", Language: "go"},
	}
	edges := []graph.Edge{
		{From: "b.go::C1", To: "b.go::Setter", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "b.go::C2", To: "b.go::Setter", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "b.go::C3", To: "b.go::Setter", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "b.go::Logic", To: "external::dep", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
	}
	g := NewGraph(nodes, edges)
	gaps, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	got := map[string]bool{}
	for _, gp := range gaps {
		got[gp.Node.ID] = true
	}
	if got["b.go::Setter"] {
		t.Errorf("trivial accessor b.go::Setter (span 4, 0 callees) should be excluded; got %v", gapIDs(gaps))
	}
	if !got["b.go::Logic"] {
		t.Errorf("b.go::Logic (span 4, delegates to a callee) should be a gap; got %v", gapIDs(gaps))
	}
}

func TestUntestedGaps_complexityBreaksTies(t *testing.T) {
	// Two exported functions with identical connectivity (equal fan-in and
	// equal blast radius) but very different size. Today they tie on score and
	// the ID tiebreak puts the short accessor first. After L1 the longer,
	// branchier function must rank first: connection count alone must not
	// surface a trivial method above complex logic.
	shortAcc := fn("svc.go::Aaccessor", "svc.go", 10, 5)  // span 5
	branchy := fn("svc.go::Zalgorithm", "svc.go", 30, 40) // span 40
	nodes := []graph.Node{
		{ID: "svc.go", Kind: graph.KindFile, Name: "svc.go", Path: "svc.go", Language: "go"},
		shortAcc, branchy,
		fn("a.go::A", "a.go", 1, 10),
		fn("b.go::B", "b.go", 1, 10),
		fn("c.go::C", "c.go", 1, 10),
		fn("d.go::D", "d.go", 1, 10),
	}
	edges := []graph.Edge{
		{From: "a.go::A", To: "svc.go::Aaccessor", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "b.go::B", To: "svc.go::Aaccessor", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "c.go::C", To: "svc.go::Zalgorithm", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
		{From: "d.go::D", To: "svc.go::Zalgorithm", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
	}
	g := NewGraph(nodes, edges)
	gaps, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	ids := gapIDs(gaps)
	if len(ids) < 2 || ids[0] != "svc.go::Zalgorithm" {
		t.Fatalf("ranking = %v, want svc.go::Zalgorithm first (complexity breaks the tie)", ids)
	}
}

func TestUntestedGaps_defaults(t *testing.T) {
	g := gapFixture()
	a, err := g.UntestedGaps(GapOptions{})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	b, err := g.UntestedGaps(GapOptions{MinLines: 4, Depth: 2})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	if len(a) != len(b) {
		t.Errorf("zero-value options: %d gaps, explicit defaults: %d", len(a), len(b))
	}
}

// domainFixture: billing skill documents svc.go only.
func domainFixture() *Graph {
	nodes := []graph.Node{
		{ID: "svc.go", Kind: graph.KindFile, Name: "svc.go", Path: "svc.go", Language: "go"},
		{ID: "other.go", Kind: graph.KindFile, Name: "other.go", Path: "other.go", Language: "go"},
		fn("svc.go::Pay", "svc.go", 10, 10),
		fn("other.go::Misc", "other.go", 10, 10),
		{ID: "skill::billing", Kind: graph.KindSkill, Name: "billing", Path: ".claude/skills/billing/SKILL.md", Line: 1},
		{ID: "skill::http", Kind: graph.KindSkill, Name: "http", Path: ".claude/skills/http/SKILL.md", Line: 1},
	}
	edges := []graph.Edge{
		{From: "skill::billing", To: "svc.go", Kind: graph.EdgeDocuments, Confidence: graph.ConfExact},
	}
	return NewGraph(nodes, edges)
}

func TestUntestedGaps_domainFilter(t *testing.T) {
	g := domainFixture()
	gaps, err := g.UntestedGaps(GapOptions{Domain: "billing"})
	if err != nil {
		t.Fatalf("UntestedGaps: %v", err)
	}
	if len(gaps) != 1 || gaps[0].Node.ID != "svc.go::Pay" {
		t.Errorf("domain=billing gaps = %v, want only svc.go::Pay", gapIDs(gaps))
	}
}

func TestUntestedGaps_unknownDomainListsAvailable(t *testing.T) {
	g := domainFixture()
	_, err := g.UntestedGaps(GapOptions{Domain: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown domain")
	}
	for _, want := range []string{"nope", "billing", "http"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestFormatGaps(t *testing.T) {
	gaps := []Gap{
		{
			Node: graph.Node{ID: "svc.go::Pay", Kind: graph.KindFunction, Name: "Pay",
				Path: "svc.go", Line: 10, EndLine: 19, Language: "go",
				Params: "(amount int)", ReturnType: "error", Exported: true},
			FanIn: 2, BlastRadius: 5, Span: 10, Score: 15,
		},
	}
	out := FormatGaps(gaps)
	for _, want := range []string{"Pay(amount int) error", "svc.go:10", "| 2 |", "| 5 |", "| 15 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatGaps output missing %q:\n%s", want, out)
		}
	}
	if empty := FormatGaps(nil); !strings.Contains(empty, "No untested public symbols") {
		t.Errorf("empty FormatGaps = %q", empty)
	}
}

func TestUntestedGapsCoverageMode(t *testing.T) {
	// Two exported funcs with no test linkage. With coverage: one fully
	// covered (excluded), one 50% covered (included, uncovered factor 0.5).
	// fn(id, path, line, span): Full → lines 1..10, Half → lines 11..20.
	nodes := []graph.Node{
		{ID: "svc.go", Kind: graph.KindFile, Name: "svc.go", Path: "svc.go", Language: "go"},
		fn("svc.go::Full", "svc.go", 1, 10),
		fn("svc.go::Half", "svc.go", 11, 10),
	}
	g := NewGraph(nodes, nil)
	cov := fakeCoverer{
		"svc.go|1|10":  {1.0, true},
		"svc.go|11|20": {0.5, true},
	}
	gaps, err := g.UntestedGaps(GapOptions{Coverage: cov})
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].Node.Name != "Half" {
		t.Fatalf("want only Half, got %+v", gaps)
	}
	if gaps[0].Covered != 0.5 {
		t.Fatalf("covered = %v, want 0.5", gaps[0].Covered)
	}
}

// fakeCoverer implements SymbolCoverer for tests.
type fakeCoverer map[string][2]interface{}

func (f fakeCoverer) SymbolCoverage(path string, start, end int) (float64, bool) {
	v, ok := f[fmt.Sprintf("%s|%d|%d", path, start, end)]
	if !ok {
		return 0, false
	}
	return v[0].(float64), v[1].(bool)
}

func TestIsTestPath_typescript(t *testing.T) {
	cases := map[string]bool{
		"src/slug.test.ts":         true,
		"src/Button.spec.tsx":      true,
		"src/__tests__/slug.ts":    true,
		"src/slug.ts":              false,
		"src/latest.ts":            false,
		"internal/store/x_test.go": true,
		"tests/test_parser.py":     true,
	}
	for p, want := range cases {
		if got := isTestPath(p); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestClassMethods(t *testing.T) {
	nodes := []graph.Node{
		{ID: "billing.Order", Name: "Order", Kind: graph.KindClass},
		{ID: "billing.Order.Pay", Name: "Order.Pay", Kind: graph.KindFunction, Exported: true},
		{ID: "billing.Order.refund", Name: "Order.refund", Kind: graph.KindFunction, Exported: false},
		{ID: "billing.Order.Cancel", Name: "Order.Cancel", Kind: graph.KindFunction, Exported: true},
		{ID: "billing.Other", Name: "Other", Kind: graph.KindClass},
	}
	edges := []graph.Edge{
		{From: "billing.Order", To: "billing.Order.Pay", Kind: graph.EdgeContains},
		{From: "billing.Order", To: "billing.Order.refund", Kind: graph.EdgeContains},
		{From: "billing.Order", To: "billing.Order.Cancel", Kind: graph.EdgeContains},
	}
	g := NewGraph(nodes, edges)

	got := g.ClassMethods("billing.Order")
	var names []string
	for _, n := range got {
		names = append(names, n.Name)
	}
	want := []string{"Order.Cancel", "Order.Pay"} // exported only, sorted by ID
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("ClassMethods = %v, want %v", names, want)
	}
	if len(g.ClassMethods("billing.Unknown")) != 0 {
		t.Errorf("unknown class should yield no methods")
	}
	if len(g.ClassMethods("billing.Other")) != 0 {
		t.Errorf("class with no children should yield no methods")
	}
}
