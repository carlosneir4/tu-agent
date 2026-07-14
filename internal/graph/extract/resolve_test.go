package extract

import (
	"slices"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

func buildWorld() ([]graph.Node, []graph.FileMeta) {
	mk := func(path, pkg string, classes ...string) ([]graph.Node, graph.FileMeta) {
		nodes := []graph.Node{{ID: path, Kind: graph.KindFile, Name: path, Path: path}}
		for _, c := range classes {
			kind := graph.KindClass
			if isTestClassName(c) {
				kind = graph.KindTest
			}
			nodes = append(nodes, graph.Node{ID: path + "::" + c, Kind: kind, Name: c, Path: path})
		}
		return nodes, graph.FileMeta{Path: path, Language: "java", Package: pkg}
	}
	var nodes []graph.Node
	var metas []graph.FileMeta
	add := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
	add(mk("core/BaseService.java", "com.acme.core", "BaseService"))
	add(mk("core/Helper.java", "com.acme.core", "Helper"))
	add(mk("billing/AbstractBilling.java", "com.acme.billing", "AbstractBilling"))
	add(mk("billing/InvoiceService.java", "com.acme.billing", "InvoiceService"))
	add(mk("billing/InvoiceServiceTest.java", "com.acme.billing", "InvoiceServiceTest"))
	add(mk("a/Mapper.java", "com.acme.a", "Mapper"))
	add(mk("b/Mapper.java", "com.acme.b", "Mapper"))
	nodes = append(nodes, graph.Node{ID: "core/Helper.java::Helper.assist", Kind: graph.KindFunction, Name: "Helper.assist", Path: "core/Helper.java"})
	return nodes, metas
}

func TestResolve(t *testing.T) {
	nodes, metas := buildWorld()
	for i := range metas {
		if metas[i].Path == "billing/InvoiceService.java" {
			metas[i].Imports = []string{"com.acme.core.BaseService", "com.acme.core.*"}
		}
	}
	refs := []graph.Ref{
		{FromID: "billing/InvoiceService.java::InvoiceService", Kind: graph.EdgeExtends, Name: "BaseService"},
		{FromID: "billing/InvoiceService.java::InvoiceService", Kind: graph.EdgeImplements, Name: "AbstractBilling"},
		{FromID: "billing/AbstractBilling.java::AbstractBilling", Kind: graph.EdgeExtends, Name: "com.acme.core.Helper"},
		{FromID: "billing/AbstractBilling.java::AbstractBilling", Kind: graph.EdgeImplements, Name: "Mapper"},
		{FromID: "billing/InvoiceService.java::InvoiceService", Kind: graph.EdgeCalls, Name: "assist"},
		{FromID: "billing/InvoiceService.java::InvoiceService", Kind: graph.EdgeExtends, Name: "SpringBean"},
	}
	edges, _ := ResolveWithNodes(nodes, metas, refs, "")

	type want struct {
		to   string
		kind graph.EdgeKind
		conf graph.Confidence
	}
	wants := []want{
		{"core/BaseService.java::BaseService", graph.EdgeExtends, graph.ConfExact},
		{"billing/AbstractBilling.java::AbstractBilling", graph.EdgeImplements, graph.ConfExact},
		{"core/Helper.java::Helper", graph.EdgeExtends, graph.ConfExact},
		{"a/Mapper.java::Mapper", graph.EdgeImplements, graph.ConfLow},
		{"b/Mapper.java::Mapper", graph.EdgeImplements, graph.ConfLow},
		{"core/Helper.java::Helper.assist", graph.EdgeCalls, graph.ConfHigh},
		{"billing/InvoiceServiceTest.java::InvoiceServiceTest", graph.EdgeTestedBy, graph.ConfHigh},
	}
	find := func(w want) bool {
		for _, e := range edges {
			if e.To == w.to && e.Kind == w.kind && e.Confidence == w.conf {
				return true
			}
		}
		return false
	}
	for _, w := range wants {
		if !find(w) {
			t.Errorf("missing edge to=%s kind=%s conf=%s\nall edges: %+v", w.to, w.kind, w.conf, edges)
		}
	}
	for _, e := range edges {
		if e.To == "" || e.From == "" {
			t.Errorf("edge with empty endpoint: %+v", e)
		}
		// After Task 1: a bare "SpringBean" must never be an edge target as-is.
		if e.Kind == graph.EdgeExtends && e.To == "SpringBean" {
			t.Errorf("unqualified external ref must not produce a raw-name edge: %+v", e)
		}
		// Any external stub target must use the external:: prefix convention.
		if strings.HasPrefix(e.To, "external::") && e.Confidence != graph.ConfExact {
			t.Errorf("external stub edge must be ConfExact: %+v", e)
		}
	}
}

func TestResolve_freeFunctionCall(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a.go", Kind: graph.KindFile, Name: "a.go", Path: "a.go", Language: "go"},
		{ID: "a.go::Caller", Kind: graph.KindFunction, Name: "Caller", Path: "a.go", Language: "go"},
		{ID: "a.go::Helper", Kind: graph.KindFunction, Name: "Helper", Path: "a.go", Language: "go"},
	}
	metas := []graph.FileMeta{{Path: "a.go", Language: "go", Package: "."}}
	refs := []graph.Ref{{FromID: "a.go::Caller", Kind: graph.EdgeCalls, Name: "Helper"}}

	edges, _ := ResolveWithNodes(nodes, metas, refs, "")

	found := false
	for _, e := range edges {
		if e.From == "a.go::Caller" && e.To == "a.go::Helper" && e.Kind == graph.EdgeCalls {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected calls edge Caller->Helper, got %+v", edges)
	}
}

// TestResolveSuppressesLoggerCollision: an unqualified-name call resolves by
// method name alone (no receiver types), so LOGGER.error() collided with a
// project method named error(). Capturing the receiver lets the resolver drop
// logging-level calls on a logger-named receiver; a real receiver still binds.
func TestResolveSuppressesLoggerCollision(t *testing.T) {
	nodes := []graph.Node{
		{ID: "ing/IngestionResult.java", Kind: graph.KindFile, Name: "ing/IngestionResult.java", Path: "ing/IngestionResult.java", Language: "java"},
		{ID: "ing/IngestionResult.java::IngestionResult", Kind: graph.KindClass, Name: "IngestionResult", Path: "ing/IngestionResult.java"},
		{ID: "ing/IngestionResult.java::IngestionResult.error", Kind: graph.KindFunction, Name: "IngestionResult.error", Path: "ing/IngestionResult.java"},
		{ID: "ing/Ingestor.java", Kind: graph.KindFile, Name: "ing/Ingestor.java", Path: "ing/Ingestor.java", Language: "java"},
		{ID: "ing/Ingestor.java::Ingestor", Kind: graph.KindClass, Name: "Ingestor", Path: "ing/Ingestor.java"},
	}
	metas := []graph.FileMeta{
		{Path: "ing/IngestionResult.java", Language: "java", Package: "com.acme.ing"},
		{Path: "ing/Ingestor.java", Language: "java", Package: "com.acme.ing"},
	}
	const target = "ing/IngestionResult.java::IngestionResult.error"
	bindsError := func(recv string) bool {
		refs := []graph.Ref{{FromID: "ing/Ingestor.java::Ingestor", Kind: graph.EdgeCalls, Name: "error", Recv: recv}}
		edges, _ := ResolveWithNodes(nodes, metas, refs, "")
		for _, e := range edges {
			if e.To == target && e.Kind == graph.EdgeCalls {
				return true
			}
		}
		return false
	}
	if bindsError("LOGGER") {
		t.Errorf("LOGGER.error() must not bind to project method %s", target)
	}
	if bindsError("log") {
		t.Errorf("log.error() must not bind to project method %s", target)
	}
	if !bindsError("result") {
		t.Errorf("result.error() (real receiver) should bind to %s", target)
	}
}

func TestResolveGoImports(t *testing.T) {
	const modulePath = "github.com/acme/app"
	nodes := []graph.Node{
		// Imported package has NO type nodes, only a file node: this proves we
		// resolve imports at file granularity, not via the type-only byPkg index.
		{ID: "internal/codegen/gen.go", Kind: graph.KindFile, Name: "internal/codegen/gen.go", Path: "internal/codegen/gen.go", Language: "go"},
		{ID: "cmd/app/main.go", Kind: graph.KindFile, Name: "cmd/app/main.go", Path: "cmd/app/main.go", Language: "go"},
	}
	metas := []graph.FileMeta{
		{Path: "internal/codegen/gen.go", Language: "go", Package: "internal/codegen"},
		{Path: "cmd/app/main.go", Language: "go", Package: "cmd/app", Imports: []string{
			"context",                              // stdlib, external
			"github.com/acme/app/internal/codegen", // in-module
			"github.com/spf13/cobra",               // third-party, external
		}},
	}

	edges, _ := ResolveWithNodes(nodes, metas, nil, modulePath)

	var found bool
	for _, e := range edges {
		if e.Kind == graph.EdgeImports && e.From == "cmd/app/main.go" && e.To == "internal/codegen/gen.go" {
			found = true
		}
		if e.Kind == graph.EdgeImports && (e.To == "context" || e.To == "github.com/spf13/cobra") {
			t.Errorf("external import must not produce an edge: %+v", e)
		}
	}
	if !found {
		t.Errorf("in-module Go import edge missing; edges: %+v", edges)
	}
}

func TestResolve_ExternalStub(t *testing.T) {
	nodes := []graph.Node{
		{ID: "svc/MyService.java", Kind: graph.KindFile, Name: "svc/MyService.java", Path: "svc/MyService.java", Language: "java"},
		{ID: "svc/MyService.java::MyService", Kind: graph.KindClass, Name: "MyService", Path: "svc/MyService.java", Language: "java"},
	}
	metas := []graph.FileMeta{{
		Path: "svc/MyService.java", Language: "java", Package: "com.acme.svc",
		Imports: []string{"com.framework.BaseContent"},
	}}
	refs := []graph.Ref{
		{FromID: "svc/MyService.java::MyService", Kind: graph.EdgeExtends, Name: "BaseContent"},
	}
	edges, _ := ResolveWithNodes(nodes, metas, refs, "")

	var found bool
	for _, e := range edges {
		if e.From == "svc/MyService.java::MyService" && e.Kind == graph.EdgeExtends &&
			e.To == "external::com.framework.BaseContent" && e.Confidence == graph.ConfExact {
			found = true
		}
	}
	if !found {
		t.Errorf("expected EdgeExtends to external stub, got: %+v", edges)
	}
}

func TestResolve_OverridesEdge(t *testing.T) {
	nodes := []graph.Node{
		{ID: "p/Parent.java", Kind: graph.KindFile, Name: "p/Parent.java", Path: "p/Parent.java", Language: "java"},
		{ID: "p/Parent.java::Parent", Kind: graph.KindClass, Name: "Parent", Path: "p/Parent.java", Language: "java"},
		{ID: "p/Parent.java::Parent.process", Kind: graph.KindFunction, Name: "Parent.process", Path: "p/Parent.java", Language: "java"},
		{ID: "p/Parent.java::Parent.helper", Kind: graph.KindFunction, Name: "Parent.helper", Path: "p/Parent.java", Language: "java"},
		{ID: "c/Child.java", Kind: graph.KindFile, Name: "c/Child.java", Path: "c/Child.java", Language: "java"},
		{ID: "c/Child.java::Child", Kind: graph.KindClass, Name: "Child", Path: "c/Child.java", Language: "java"},
		{ID: "c/Child.java::Child.process", Kind: graph.KindFunction, Name: "Child.process", Path: "c/Child.java", Language: "java"},
	}
	metas := []graph.FileMeta{
		{Path: "p/Parent.java", Language: "java", Package: "com.acme.p"},
		{Path: "c/Child.java", Language: "java", Package: "com.acme.c", Imports: []string{"com.acme.p.Parent"}},
	}
	refs := []graph.Ref{
		{FromID: "c/Child.java::Child", Kind: graph.EdgeExtends, Name: "Parent"},
	}
	edges, _ := ResolveWithNodes(nodes, metas, refs, "")

	var foundOverride, foundBadOverride bool
	for _, e := range edges {
		if e.Kind == graph.EdgeOverrides &&
			e.From == "c/Child.java::Child.process" &&
			e.To == "p/Parent.java::Parent.process" &&
			e.Confidence == graph.ConfHigh {
			foundOverride = true
		}
		if e.Kind == graph.EdgeOverrides && e.To == "p/Parent.java::Parent.helper" {
			foundBadOverride = true
		}
	}
	if !foundOverride {
		t.Errorf("expected overrides edge Child.process -> Parent.process; edges: %+v", edges)
	}
	if foundBadOverride {
		t.Errorf("unexpected overrides edge to Parent.helper; edges: %+v", edges)
	}
}

func TestResolve_InheritedCall(t *testing.T) {
	// Child extends Parent which defines process(). Child.run() calls process()
	// without Child defining it. The call must resolve to Parent.process at
	// ConfMedium (inherited, not locally visible).
	nodes := []graph.Node{
		{ID: "p/Parent.java", Kind: graph.KindFile, Name: "p/Parent.java", Path: "p/Parent.java", Language: "java"},
		{ID: "p/Parent.java::Parent", Kind: graph.KindClass, Name: "Parent", Path: "p/Parent.java", Language: "java"},
		{ID: "p/Parent.java::Parent.process", Kind: graph.KindFunction, Name: "Parent.process", Path: "p/Parent.java", Language: "java"},
		{ID: "c/Child.java", Kind: graph.KindFile, Name: "c/Child.java", Path: "c/Child.java", Language: "java"},
		{ID: "c/Child.java::Child", Kind: graph.KindClass, Name: "Child", Path: "c/Child.java", Language: "java"},
		{ID: "c/Child.java::Child.run", Kind: graph.KindFunction, Name: "Child.run", Path: "c/Child.java", Language: "java"},
	}
	metas := []graph.FileMeta{
		{Path: "p/Parent.java", Language: "java", Package: "com.acme.p"},
		{Path: "c/Child.java", Language: "java", Package: "com.acme.c", Imports: []string{"com.acme.p.Parent"}},
	}
	refs := []graph.Ref{
		{FromID: "c/Child.java::Child", Kind: graph.EdgeExtends, Name: "Parent"},
		{FromID: "c/Child.java::Child.run", Kind: graph.EdgeCalls, Name: "process"},
	}
	edges, _ := ResolveWithNodes(nodes, metas, refs, "")

	var found bool
	for _, e := range edges {
		if e.Kind == graph.EdgeCalls &&
			e.From == "c/Child.java::Child.run" &&
			e.To == "p/Parent.java::Parent.process" &&
			e.Confidence == graph.ConfMedium {
			found = true
		}
	}
	if !found {
		t.Errorf("expected inherited call Child.run -> Parent.process (ConfMedium); edges: %+v", edges)
	}
}

func TestResolveImportsEdges(t *testing.T) {
	nodes, metas := buildWorld()
	for i := range metas {
		if metas[i].Path == "billing/InvoiceService.java" {
			metas[i].Imports = []string{"com.acme.core.BaseService", "org.external.Thing"}
		}
	}
	edges, _ := ResolveWithNodes(nodes, metas, nil, "")
	var found bool
	for _, e := range edges {
		if e.Kind == graph.EdgeImports && e.From == "billing/InvoiceService.java" && e.To == "core/BaseService.java" {
			found = true
		}
		if e.Kind == graph.EdgeImports && e.To == "org.external.Thing" {
			t.Errorf("external import must not produce an edge: %+v", e)
		}
	}
	if !found {
		t.Errorf("file-level imports edge missing; edges: %+v", edges)
	}
}

func TestResolveWithNodes_GraphQLSpreads(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a.ts::Card_item", Kind: graph.KindGraphQLFragment, Name: "Card_item", Path: "a.ts"},
		{ID: "b.ts::Sub_fields", Kind: graph.KindGraphQLFragment, Name: "Sub_fields", Path: "b.ts"},
	}
	refs := []graph.Ref{
		{FromID: "a.ts::Card_item", Kind: graph.EdgeSpreads, Name: "Sub_fields"},
		{FromID: "a.ts::Card_item", Kind: graph.EdgeSpreads, Name: "Unknown_ext"}, // no node -> no edge
	}
	edges, _ := ResolveWithNodes(nodes, nil, refs, "")
	var got int
	for _, e := range edges {
		if e.Kind == graph.EdgeSpreads {
			got++
			if e.From != "a.ts::Card_item" || e.To != "b.ts::Sub_fields" {
				t.Errorf("spreads edge wrong: %+v", e)
			}
		}
	}
	if got != 1 {
		t.Fatalf("want exactly 1 resolved spreads edge, got %d", got)
	}
}

func TestResolveWithNodes_GraphQLOnType(t *testing.T) {
	nodes := []graph.Node{
		{ID: "schema.graphql::Article", Kind: graph.KindGraphQLType, Name: "Article", ReturnType: "type"},
		{ID: "a.ts::gql::Card_article", Kind: graph.KindGraphQLFragment, Name: "Card_article", ReturnType: "Article"},
		{ID: "b.ts::gql::Ghost", Kind: graph.KindGraphQLFragment, Name: "Ghost", ReturnType: "MissingType"},
		{ID: "q.ts::gql::Feed", Kind: graph.KindGraphQLOperation, Name: "Feed", ReturnType: "query"},
	}
	edges, _ := ResolveWithNodes(nodes, nil, nil, "")
	var got int
	for _, e := range edges {
		if e.Kind == graph.EdgeOnType {
			got++
			if e.From != "a.ts::gql::Card_article" || e.To != "schema.graphql::Article" {
				t.Errorf("on_type edge wrong: %+v", e)
			}
		}
	}
	if got != 1 {
		t.Fatalf("want exactly 1 on_type edge, got %d (unknown on-type and operations must not resolve)", got)
	}
}

func TestResolve_tsSiblingTestedBy(t *testing.T) {
	metas := []graph.FileMeta{
		{Path: "src/slug.ts", Language: "typescript", Package: "src.slug"},
		{Path: "src/slug.test.ts", Language: "typescript", Package: "src.slug.test"},
		{Path: "src/fmt.ts", Language: "typescript", Package: "src.fmt"},
		{Path: "src/__tests__/fmt.test.ts", Language: "typescript", Package: "src.__tests__.fmt.test"},
		{Path: "pkg/parser.py", Language: "python", Package: "pkg.parser"},
	}
	edges, _ := ResolveWithNodes(nil, metas, nil, "")
	want := []graph.Edge{
		{From: "src/slug.ts", To: "src/slug.test.ts", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
		{From: "src/fmt.ts", To: "src/__tests__/fmt.test.ts", Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh},
	}
	for _, w := range want {
		if !slices.Contains(edges, w) {
			t.Errorf("missing edge %+v", w)
		}
	}
}

func TestStripTestSuffix(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"OrderServiceShippingTest", "OrderServiceShipping", true},
		{"OrderServiceTests", "OrderService", true},
		{"OrderServiceIT", "OrderService", true},
		{"OrderService", "OrderService", false},
		{"TestOrderService", "TestOrderService", false}, // prefix form is the exact §3 case, not a split
	}
	for _, c := range cases {
		got, ok := stripTestSuffix(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("stripTestSuffix(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestResolve_splitTestClassFallback(t *testing.T) {
	// mk builds a one-class file; if methods are given they become KindFunction
	// nodes named "Class.method". A class is KindTest when its name looks like a
	// test class.
	mk := func(path, pkg, name string, methods ...string) ([]graph.Node, graph.FileMeta) {
		kind := graph.KindClass
		if isTestClassName(name) {
			kind = graph.KindTest
		}
		nodes := []graph.Node{
			{ID: path, Kind: graph.KindFile, Name: path, Path: path},
			{ID: path + "::" + name, Kind: kind, Name: name, Path: path},
		}
		for _, m := range methods {
			nodes = append(nodes, graph.Node{
				ID: path + "::" + name + "." + m, Kind: graph.KindFunction,
				Name: name + "." + m, Path: path,
			})
		}
		return nodes, graph.FileMeta{Path: path, Language: "java", Package: pkg}
	}

	hasEdge := func(edges []graph.Edge, from, to string, conf graph.Confidence) bool {
		for _, e := range edges {
			if e.Kind == graph.EdgeTestedBy && e.From == from && e.To == to && e.Confidence == conf {
				return true
			}
		}
		return false
	}
	anyTestedBy := func(edges []graph.Edge, to string) bool {
		for _, e := range edges {
			if e.Kind == graph.EdgeTestedBy && e.To == to {
				return true
			}
		}
		return false
	}

	t.Run("import evidence links at medium", func(t *testing.T) {
		var nodes []graph.Node
		var metas []graph.FileMeta
		addw := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
		addw(mk("svc/OrderService.java", "com.acme.svc", "OrderService", "ship"))
		tn, tm := mk("svc/OrderServiceShippingTest.java", "com.acme.svc", "OrderServiceShippingTest", "shipsOrder")
		tm.Imports = []string{"com.acme.svc.OrderService"}
		addw(tn, tm)

		edges, _ := ResolveWithNodes(nodes, metas, nil, "")

		if !hasEdge(edges, "svc/OrderService.java::OrderService",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfMedium) {
			t.Fatalf("want medium tested_by OrderService->OrderServiceShippingTest; edges: %+v", edges)
		}
	})

	t.Run("call evidence links at medium", func(t *testing.T) {
		var nodes []graph.Node
		var metas []graph.FileMeta
		addw := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
		addw(mk("svc/OrderService.java", "com.acme.svc", "OrderService", "ship"))
		addw(mk("svc/OrderServiceShippingTest.java", "com.acme.svc", "OrderServiceShippingTest", "shipsOrder"))
		refs := []graph.Ref{{
			FromID: "svc/OrderServiceShippingTest.java::OrderServiceShippingTest.shipsOrder",
			Kind:   graph.EdgeCalls, Name: "ship",
		}}

		edges, _ := ResolveWithNodes(nodes, metas, refs, "")

		if !hasEdge(edges, "svc/OrderService.java::OrderService",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfMedium) {
			t.Fatalf("want medium tested_by via call evidence; edges: %+v", edges)
		}
	})

	t.Run("no evidence does not link", func(t *testing.T) {
		var nodes []graph.Node
		var metas []graph.FileMeta
		addw := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
		addw(mk("svc/OrderService.java", "com.acme.svc", "OrderService", "ship"))
		addw(mk("svc/OrderServiceShippingTest.java", "com.acme.svc", "OrderServiceShippingTest", "shipsOrder"))

		edges, _ := ResolveWithNodes(nodes, metas, nil, "")

		if anyTestedBy(edges, "svc/OrderServiceShippingTest.java::OrderServiceShippingTest") {
			t.Fatalf("no reference => no tested_by edge; edges: %+v", edges)
		}
	})

	t.Run("longest prefix wins over shorter class", func(t *testing.T) {
		var nodes []graph.Node
		var metas []graph.FileMeta
		addw := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
		addw(mk("svc/Order.java", "com.acme.svc", "Order"))
		addw(mk("svc/OrderService.java", "com.acme.svc", "OrderService", "ship"))
		tn, tm := mk("svc/OrderServiceShippingTest.java", "com.acme.svc", "OrderServiceShippingTest", "shipsOrder")
		tm.Imports = []string{"com.acme.svc.Order", "com.acme.svc.OrderService"}
		addw(tn, tm)

		edges, _ := ResolveWithNodes(nodes, metas, nil, "")

		if !hasEdge(edges, "svc/OrderService.java::OrderService",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfMedium) {
			t.Fatalf("want link to OrderService; edges: %+v", edges)
		}
		if hasEdge(edges, "svc/Order.java::Order",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfMedium) {
			t.Fatalf("must not link to shorter prefix Order; edges: %+v", edges)
		}
	})

	t.Run("exact source class takes §3, no medium duplicate", func(t *testing.T) {
		var nodes []graph.Node
		var metas []graph.FileMeta
		addw := func(n []graph.Node, m graph.FileMeta) { nodes = append(nodes, n...); metas = append(metas, m) }
		// The exact source class OrderServiceShipping exists, so the test is its
		// exact-convention test (§3 at high), not a split of OrderService.
		addw(mk("svc/OrderService.java", "com.acme.svc", "OrderService", "ship"))
		addw(mk("svc/OrderServiceShipping.java", "com.acme.svc", "OrderServiceShipping"))
		tn, tm := mk("svc/OrderServiceShippingTest.java", "com.acme.svc", "OrderServiceShippingTest", "shipsOrder")
		tm.Imports = []string{"com.acme.svc.OrderService", "com.acme.svc.OrderServiceShipping"}
		addw(tn, tm)

		edges, _ := ResolveWithNodes(nodes, metas, nil, "")

		if !hasEdge(edges, "svc/OrderServiceShipping.java::OrderServiceShipping",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfHigh) {
			t.Fatalf("want exact §3 high edge to OrderServiceShipping; edges: %+v", edges)
		}
		if hasEdge(edges, "svc/OrderService.java::OrderService",
			"svc/OrderServiceShippingTest.java::OrderServiceShippingTest", graph.ConfMedium) {
			t.Fatalf("must not also emit a medium split edge to OrderService; edges: %+v", edges)
		}
	})
}

func TestLongestPrefixClass(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		classes []string
		want    string
	}{
		{"longest wins", "OrderServiceShipping", []string{"Order", "OrderService"}, "OrderService"},
		{"shorter when only it qualifies", "OrderShipping", []string{"Order", "OrderService"}, "Order"},
		{"camelcase boundary required", "OrderServicexyz", []string{"OrderService"}, ""},
		{"equal length excluded", "OrderService", []string{"OrderService"}, ""},
		{"no prefix", "Standalone", []string{"Order", "OrderService"}, ""},
	}
	for _, c := range cases {
		if got := longestPrefixClass(c.base, c.classes); got != c.want {
			t.Errorf("%s: longestPrefixClass(%q) = %q, want %q", c.name, c.base, got, c.want)
		}
	}
}
