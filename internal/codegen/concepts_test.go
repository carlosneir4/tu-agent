package codegen

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

func TestDiscoverConcepts_multiRootCollision(t *testing.T) {
	units := []SourceUnit{
		{Package: "packages.app.src", Path: "packages/app/src/a.ts"},
		{Package: "packages.commons.x", Path: "packages/commons/x.ts"},
		{Package: "native-packages.app.y", Path: "native-packages/app/y.ts"},
		{Package: "rigs.jest.z", Path: "rigs/jest/z.ts"},
		{Package: "unrelated.q", Path: "unrelated/q.ts"},
	}
	got := DiscoverConcepts(units, []string{"packages", "native-packages"})
	names := map[string]string{} // name -> package
	for _, c := range got {
		names[c.Name] = c.Package
	}
	// "app" leaf collides across the two roots → both qualified by package slug.
	if names["packages-app"] != "packages.app" || names["native-packages-app"] != "native-packages.app" {
		t.Fatalf("collision not qualified: %+v", names)
	}
	if _, bare := names["app"]; bare {
		t.Errorf("colliding leaf 'app' must not remain bare: %+v", names)
	}
	// "commons" is unique → stays bare.
	if names["commons"] != "packages.commons" {
		t.Errorf("unique leaf should stay bare: %+v", names)
	}
	// rigs/unrelated are not under any root → excluded.
	for _, c := range got {
		if c.Package == "rigs.jest" || c.Package == "unrelated" {
			t.Errorf("out-of-root concept leaked: %+v", c)
		}
	}
}

func TestDiscoverConcepts_singleRootUnchanged(t *testing.T) {
	units := []SourceUnit{
		{Package: "packages.app.src", Path: "packages/app/src/a.ts"},
		{Package: "packages.commons.x", Path: "packages/commons/x.ts"},
	}
	got := DiscoverConcepts(units, []string{"packages"})
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	// No cross-root collision within one root → bare leaves.
	if !names["app"] || !names["commons"] {
		t.Fatalf("single-root leaves should be bare: %+v", names)
	}
}

func TestDiscoverConcepts_emptyRoots(t *testing.T) {
	if got := DiscoverConcepts([]SourceUnit{{Package: "packages.app", Path: "p"}}, nil); got != nil {
		t.Fatalf("nil roots must return nil (domain-map fallback), got %+v", got)
	}
}

// File-level landmarks (common in Java) have Name == Path; rendering them as
// "path (path)" duplicates the path on every card. Render once instead.
func TestRenderConceptCard_DedupesLandmarkWhenNameEqualsPath(t *testing.T) {
	card := ConceptCard{
		Concept:    Concept{Name: "webapi", Package: "x.webapi", Files: []string{"a/WebApiType.java"}},
		Definition: "Web API core.",
		Landmarks:  []Landmark{{Name: "a/WebApiType.java", Path: "a/WebApiType.java", Entry: true}},
	}
	out := RenderConceptCard(card)
	if strings.Contains(out, "a/WebApiType.java (a/WebApiType.java)") {
		t.Errorf("landmark must not repeat the path when Name == Path; got:\n%s", out)
	}
	if !strings.Contains(out, "- a/WebApiType.java — entry point") {
		t.Errorf("landmark should render once with the entry-point marker; got:\n%s", out)
	}
}

// When the landmark name is a symbol distinct from its file path (e.g. a Go
// function), keep the "Name (Path)" form so the location is still shown.
func TestRenderConceptCard_KeepsNameAndPathWhenDifferent(t *testing.T) {
	card := ConceptCard{
		Concept:    Concept{Name: "billing", Package: "x.billing", Files: []string{"b/Invoice.go"}},
		Definition: "Billing.",
		Landmarks:  []Landmark{{Name: "Process", Path: "b/Invoice.go"}},
	}
	out := RenderConceptCard(card)
	if !strings.Contains(out, "Process (b/Invoice.go)") {
		t.Errorf("when Name != Path, keep 'Name (Path)'; got:\n%s", out)
	}
}

func TestDiscoverConceptsByRoot(t *testing.T) {
	units := []SourceUnit{
		{Path: "src/c/P.java", Package: "com.acme.shop.catalog"},
		{Path: "src/c/Q.java", Package: "com.acme.shop.catalog.search"}, // depth>1 folds into catalog
		{Path: "src/o/O.java", Package: "com.acme.shop.orders"},
		{Path: "src/r/R.java", Package: "com.acme.shop"}, // residue: directly in root
		{Path: "src/x/X.java", Package: "com.acme.util"}, // outside root: excluded
		{Path: "src/n/N.java", Package: ""},              // no package: excluded
	}

	got := DiscoverConcepts(units, []string{"com.acme.shop"})

	want := []Concept{
		{Name: "catalog", Package: "com.acme.shop.catalog", Files: []string{"src/c/P.java", "src/c/Q.java"}},
		{Name: "orders", Package: "com.acme.shop.orders", Files: []string{"src/o/O.java"}},
		{Name: "shop", Package: "com.acme.shop", Files: []string{"src/r/R.java"}}, // residue named by root leaf
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverConcepts =\n%v\nwant\n%v", got, want)
	}
}

func TestDiscoverConceptsEmptyRootReturnsNil(t *testing.T) {
	if got := DiscoverConcepts([]SourceUnit{{Path: "a", Package: "p"}}, []string{}); got != nil {
		t.Errorf("empty roots: got %v, want nil (caller falls back to domain map)", got)
	}
}

func TestConceptsFromDomains(t *testing.T) {
	domains := []Domain{
		{Name: "billing", Package: "com.acme.billing", Files: []string{"b1", "b2"}},
		{Name: "split", Package: "com.acme.split", Files: nil}, // parent marker: skipped
		{Name: "split-a", Package: "com.acme.split.a", Files: []string{"s1"}, Parent: "split"},
	}
	got := ConceptsFromDomains(domains)
	want := []Concept{
		{Name: "billing", Package: "com.acme.billing", Files: []string{"b1", "b2"}},
		{Name: "split-a", Package: "com.acme.split.a", Files: []string{"s1"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConceptsFromDomains = %v, want %v", got, want)
	}
}

func conceptGraphFixture() ([]graph.Node, []graph.Edge) {
	nodes := []graph.Node{
		{ID: "c:Product", Kind: graph.KindClass, Name: "Product", Path: "src/c/Product.java", Exported: true},
		{ID: "c:Product.price", Kind: graph.KindFunction, Name: "price", Path: "src/c/Product.java"},
		{ID: "c:Helper", Kind: graph.KindClass, Name: "Helper", Path: "src/c/Helper.java"},
		{ID: "o:Order", Kind: graph.KindClass, Name: "Order", Path: "src/o/Order.java", Exported: true},
		{ID: "i:Refundable", Kind: graph.KindClass, Name: "Refundable", Path: "src/i/Refundable.java", Exported: true},
		{ID: "g:GiftCard", Kind: graph.KindClass, Name: "GiftCard", Path: "src/g/GiftCard.java", Exported: true},
	}
	edges := []graph.Edge{
		{From: "c:Product", To: "c:Product.price", Kind: graph.EdgeContains},
		// method fan-in aggregates to Product; one caller is from another concept -> entry
		{From: "o:Order", To: "c:Product.price", Kind: graph.EdgeCalls},
		{From: "c:Helper", To: "c:Product.price", Kind: graph.EdgeCalls},
		// Order implements Refundable; so does GiftCard (different concept) -> shared trait
		{From: "o:Order", To: "i:Refundable", Kind: graph.EdgeImplements},
		{From: "g:GiftCard", To: "i:Refundable", Kind: graph.EdgeImplements},
	}
	return nodes, edges
}

func TestBuildConceptCardsLandmarksAggregateAndMarkEntries(t *testing.T) {
	nodes, edges := conceptGraphFixture()
	concepts := []Concept{
		{Name: "catalog", Package: "p.catalog", Files: []string{"src/c/Product.java", "src/c/Helper.java"}},
		{Name: "orders", Package: "p.orders", Files: []string{"src/o/Order.java"}},
		{Name: "gifts", Package: "p.gifts", Files: []string{"src/g/GiftCard.java"}},
	}
	cards := BuildConceptCards(concepts, nodes, edges, 10)

	cat := cards[0]
	if len(cat.Landmarks) == 0 || cat.Landmarks[0].Name != "Product" {
		t.Fatalf("catalog landmarks = %+v, want Product first (aggregated fan-in 2)", cat.Landmarks)
	}
	if cat.Landmarks[0].FanIn != 2 {
		t.Errorf("Product fan-in = %d, want 2 (method calls aggregate to type)", cat.Landmarks[0].FanIn)
	}
	if !cat.Landmarks[0].Entry {
		t.Error("Product must be an entry point (called from orders)")
	}
}

func TestBuildConceptCardsDetectSharedTraits(t *testing.T) {
	nodes, edges := conceptGraphFixture()
	concepts := []Concept{
		{Name: "orders", Package: "p.orders", Files: []string{"src/o/Order.java"}},
		{Name: "gifts", Package: "p.gifts", Files: []string{"src/g/GiftCard.java"}},
	}
	cards := BuildConceptCards(concepts, nodes, edges, 10)

	ord := cards[0]
	if len(ord.Traits) != 1 || ord.Traits[0].Name != "Refundable" {
		t.Fatalf("orders traits = %+v, want [Refundable]", ord.Traits)
	}
	if !reflect.DeepEqual(ord.Traits[0].OtherImplementers, []string{"GiftCard"}) {
		t.Errorf("other implementers = %v, want [GiftCard]", ord.Traits[0].OtherImplementers)
	}
}

func TestRenderConceptCardFormat(t *testing.T) {
	card := ConceptCard{
		Concept:    Concept{Name: "catalog", Package: "p.catalog", Files: []string{"a", "b"}},
		Definition: "Product browsing and pricing.",
		Landmarks: []Landmark{
			{Name: "Product", Path: "src/Product.java", FanIn: 9, Entry: true},
			{Name: "Helper", Path: "src/Helper.java", FanIn: 1},
		},
		Traits: []Trait{{Name: "Refundable", Path: "src/Refundable.java", OtherImplementers: []string{"GiftCard"}}},
	}
	got := RenderConceptCard(card)

	for _, want := range []string{
		"name: catalog",
		"description: Product browsing and pricing.",
		"# catalog (p.catalog)",
		"- Product (src/Product.java) — entry point",
		"- Helper (src/Helper.java)",
		"- Refundable — also implemented by: GiftCard",
		"get_context",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderConceptCardDeterministicFallbackDescription(t *testing.T) {
	card := ConceptCard{
		Concept:   Concept{Name: "orders", Package: "p.orders", Files: []string{"a"}},
		Landmarks: []Landmark{{Name: "Order", Path: "src/Order.java", FanIn: 3}},
	}
	got := RenderConceptCard(card)
	// description is YAML-quoted because it contains ": "
	if !strings.Contains(got, `description: "p.orders — 1 files; landmarks: Order`) {
		t.Errorf("fallback description missing in:\n%s", got)
	}
}

func TestRenderConceptCardEnforcesBudget(t *testing.T) {
	card := ConceptCard{Concept: Concept{Name: "big", Package: "p.big", Files: make([]string, 500)}}
	for i := range 50 {
		card.Landmarks = append(card.Landmarks, Landmark{
			Name: fmt.Sprintf("VeryLongTypeNameNumber%02d", i),
			Path: fmt.Sprintf("src/very/long/path/to/VeryLongTypeNameNumber%02d.java", i), FanIn: 50 - i,
		})
		card.Traits = append(card.Traits, Trait{
			Name: fmt.Sprintf("TraitNumber%02d", i), OtherImplementers: []string{"A", "B", "C", "D", "E", "F"},
		})
	}
	got := RenderConceptCard(card)
	if len(got) > 1024 {
		t.Errorf("card size = %d bytes, budget is 1024", len(got))
	}
	if n := strings.Count(got, "\n"); n > 30 {
		t.Errorf("card lines = %d, budget is 30", n)
	}
}

func TestBuildConceptCardsCapsLandmarks(t *testing.T) {
	// 4 classes with descending fan-in, cap 2: only top 2 survive.
	var nodes []graph.Node
	var edges []graph.Edge
	files := []string{}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("n:T%d", i)
		path := fmt.Sprintf("src/T%d.java", i)
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindClass, Name: fmt.Sprintf("T%d", i), Path: path})
		files = append(files, path)
		for j := 0; j < 4-i; j++ {
			caller := fmt.Sprintf("n:C%d_%d", i, j)
			nodes = append(nodes, graph.Node{ID: caller, Kind: graph.KindClass, Name: caller, Path: "src/Caller.java"})
			edges = append(edges, graph.Edge{From: caller, To: id, Kind: graph.EdgeCalls})
		}
	}
	files = append(files, "src/Caller.java")
	cards := BuildConceptCards([]Concept{{Name: "c", Package: "p", Files: files}}, nodes, edges, 2)
	if len(cards[0].Landmarks) != 2 {
		t.Fatalf("landmarks = %d, want capped at 2", len(cards[0].Landmarks))
	}
	if cards[0].Landmarks[0].Name != "T0" || cards[0].Landmarks[1].Name != "T1" {
		t.Errorf("top landmarks = %s,%s want T0,T1", cards[0].Landmarks[0].Name, cards[0].Landmarks[1].Name)
	}
}
