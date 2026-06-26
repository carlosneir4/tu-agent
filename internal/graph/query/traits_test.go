package query

import (
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

// traitsFixture models a fictional shop service:
//   - interface Billable { bill() }            (shop/Billable.java)
//   - interface Refundable extends Billable { refund() }
//   - abstract class BaseRecord { refund() }   (shared logic for Order)
//   - class Order extends BaseRecord implements Refundable
//   - class Subscription implements Refundable { refund() }
//   - class GiftCard extends Subscription      (inherited implementer)
//   - RefundService.process calls Refundable.refund
func traitsFixture() *Graph {
	nodes := []graph.Node{
		{ID: "shop/Billable.java", Kind: graph.KindFile, Name: "Billable.java", Path: "shop/Billable.java"},
		{ID: "shop/Billable.java::Billable", Kind: graph.KindClass, Name: "Billable", Path: "shop/Billable.java", Line: 3},
		{ID: "shop/Billable.java::Billable.bill", Kind: graph.KindFunction, Name: "Billable.bill", Path: "shop/Billable.java", Line: 4},
		{ID: "shop/Refundable.java", Kind: graph.KindFile, Name: "Refundable.java", Path: "shop/Refundable.java"},
		{ID: "shop/Refundable.java::Refundable", Kind: graph.KindClass, Name: "Refundable", Path: "shop/Refundable.java", Line: 3},
		{ID: "shop/Refundable.java::Refundable.refund", Kind: graph.KindFunction, Name: "Refundable.refund", Path: "shop/Refundable.java", Line: 4},
		{ID: "shop/BaseRecord.java::BaseRecord", Kind: graph.KindClass, Name: "BaseRecord", Path: "shop/BaseRecord.java", Line: 3},
		{ID: "shop/BaseRecord.java::BaseRecord.refund", Kind: graph.KindFunction, Name: "BaseRecord.refund", Path: "shop/BaseRecord.java", Line: 5},
		{ID: "shop/Order.java::Order", Kind: graph.KindClass, Name: "Order", Path: "shop/Order.java", Line: 3},
		{ID: "shop/Subscription.java::Subscription", Kind: graph.KindClass, Name: "Subscription", Path: "shop/Subscription.java", Line: 3},
		{ID: "shop/Subscription.java::Subscription.refund", Kind: graph.KindFunction, Name: "Subscription.refund", Path: "shop/Subscription.java", Line: 4},
		{ID: "shop/GiftCard.java::GiftCard", Kind: graph.KindClass, Name: "GiftCard", Path: "shop/GiftCard.java", Line: 3},
		{ID: "shop/RefundService.java::RefundService.process", Kind: graph.KindFunction, Name: "RefundService.process", Path: "shop/RefundService.java", Line: 8},
	}
	edges := []graph.Edge{
		{From: "shop/Billable.java::Billable", To: "shop/Billable.java::Billable.bill", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "shop/Refundable.java::Refundable", To: "shop/Refundable.java::Refundable.refund", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "shop/Refundable.java::Refundable", To: "shop/Billable.java::Billable", Kind: graph.EdgeExtends, Confidence: graph.ConfExact},
		{From: "shop/BaseRecord.java::BaseRecord", To: "shop/BaseRecord.java::BaseRecord.refund", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "shop/Order.java::Order", To: "shop/BaseRecord.java::BaseRecord", Kind: graph.EdgeExtends, Confidence: graph.ConfExact},
		{From: "shop/Order.java::Order", To: "shop/Refundable.java::Refundable", Kind: graph.EdgeImplements, Confidence: graph.ConfExact},
		{From: "shop/Subscription.java::Subscription", To: "shop/Refundable.java::Refundable", Kind: graph.EdgeImplements, Confidence: graph.ConfExact},
		{From: "shop/Subscription.java::Subscription", To: "shop/Subscription.java::Subscription.refund", Kind: graph.EdgeContains, Confidence: graph.ConfExact},
		{From: "shop/GiftCard.java::GiftCard", To: "shop/Subscription.java::Subscription", Kind: graph.EdgeExtends, Confidence: graph.ConfExact},
		{From: "shop/RefundService.java::RefundService.process", To: "shop/Refundable.java::Refundable.refund", Kind: graph.EdgeCalls, Confidence: graph.ConfHigh},
	}
	return NewGraph(nodes, edges)
}

func TestGraph_Traits_ByType_DirectAndInherited(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/Order.java::Order", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	if got := len(res.AsType); got != 2 {
		t.Fatalf("AsType len = %d, want 2 (Refundable direct + Billable via super-interface): %+v", got, res.AsType)
	}
	// Direct trait first.
	ref := res.AsType[0]
	if ref.Interface.Name != "Refundable" || ref.Via != "" {
		t.Errorf("AsType[0] = %s via %q, want Refundable direct", ref.Interface.Name, ref.Via)
	}
	// refund logic lives in the abstract ancestor BaseRecord, not the interface.
	if len(ref.Methods) != 1 || ref.Methods[0].ID != "shop/BaseRecord.java::BaseRecord.refund" {
		t.Errorf("Refundable methods = %+v, want BaseRecord.refund (shared logic site)", ref.Methods)
	}
	// Subscription implements it too; Order and its ancestors are excluded.
	if len(ref.OtherImplementers) != 1 || ref.OtherImplementers[0].Name != "Subscription" {
		t.Errorf("OtherImplementers = %+v, want [Subscription]", ref.OtherImplementers)
	}
	// Super-interface Billable is inherited via Refundable.
	bil := res.AsType[1]
	if bil.Interface.Name != "Billable" || bil.Via != "Refundable" {
		t.Errorf("AsType[1] = %s via %q, want Billable via Refundable", bil.Interface.Name, bil.Via)
	}
	// No override of bill() anywhere in Order's chain: the declaration is the site.
	if len(bil.Methods) != 1 || bil.Methods[0].ID != "shop/Billable.java::Billable.bill" {
		t.Errorf("Billable methods = %+v, want the interface declaration", bil.Methods)
	}
}

func TestGraph_Traits_ByType_OverrideOnTypeItself(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/Subscription.java::Subscription", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	if len(res.AsType) == 0 {
		t.Fatal("Subscription should have traits")
	}
	ref := res.AsType[0]
	if len(ref.Methods) != 1 || ref.Methods[0].ID != "shop/Subscription.java::Subscription.refund" {
		t.Errorf("methods = %+v, want Subscription's own refund override", ref.Methods)
	}
}

func TestGraph_Traits_UnknownTarget(t *testing.T) {
	g := traitsFixture()
	if _, err := g.Traits("NoSuchNode", 2, 50); err == nil {
		t.Fatal("Traits on unknown node should error")
	}
}

func TestGraph_Traits_ByInterface(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/Refundable.java::Refundable", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	ai := res.AsInterface
	if ai == nil {
		t.Fatal("AsInterface = nil, want populated for an implemented interface")
	}

	// Implementers: Order + Subscription direct, GiftCard inherited via Subscription.
	if len(ai.Implementers) != 3 {
		t.Fatalf("Implementers = %+v, want 3", ai.Implementers)
	}
	byName := map[string]Implementer{}
	for _, im := range ai.Implementers {
		byName[im.Name] = im
	}
	if im := byName["Order"]; im.Via != "" {
		t.Errorf("Order.Via = %q, want direct", im.Via)
	}
	if im := byName["GiftCard"]; im.Via != "Subscription" {
		t.Errorf("GiftCard.Via = %q, want Subscription", im.Via)
	}

	// Method declaration sites: own method + super-interface method.
	if len(ai.Methods) != 2 ||
		ai.Methods[0].ID != "shop/Refundable.java::Refundable.refund" ||
		ai.Methods[1].ID != "shop/Billable.java::Billable.bill" {
		t.Errorf("Methods = %+v, want [Refundable.refund, Billable.bill]", ai.Methods)
	}

	// Impact: RefundService.process reaches refund() via calls; implementers
	// and seeds are excluded.
	if len(ai.Impact) != 1 || ai.Impact[0].ID != "shop/RefundService.java::RefundService.process" {
		t.Errorf("Impact = %+v, want [RefundService.process]", ai.Impact)
	}
}

func TestGraph_Traits_ByInterface_NilForPlainType(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/GiftCard.java::GiftCard", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	if res.AsInterface != nil {
		t.Errorf("AsInterface = %+v, want nil — nothing implements GiftCard", res.AsInterface)
	}
}

func TestFormatTraits(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/Order.java::Order", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	out := FormatTraits(res)
	for _, want := range []string{
		"Traits of `Order`",
		"implements Refundable",
		"shop/BaseRecord.java:5", // logic site, not the interface
		"Subscription",           // other implementer
		"implements Billable (via Refundable)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTraits missing %q in:\n%s", want, out)
		}
	}

	res, err = g.Traits("shop/Refundable.java::Refundable", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	out = FormatTraits(res)
	for _, want := range []string{
		"## As interface",
		"GiftCard (shop/GiftCard.java:3) — via Subscription",
		"RefundService.process",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTraits missing %q in:\n%s", want, out)
		}
	}
}

func TestFormatTraits_NoRelations(t *testing.T) {
	g := traitsFixture()
	res, err := g.Traits("shop/Billable.java", 2, 50) // a file node: no traits
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	if out := FormatTraits(res); !strings.Contains(out, "No implements/extends relations") {
		t.Errorf("FormatTraits = %q, want the empty-result line", out)
	}
}

func TestFormatTraits_EmptyImpact(t *testing.T) {
	// An interface with implementers but no callers produces "- (none)" impact.
	g := NewGraph([]graph.Node{
		{ID: "a/I.java::I", Kind: graph.KindClass, Name: "I", Path: "a/I.java", Line: 1},
		{ID: "a/A.java::A", Kind: graph.KindClass, Name: "A", Path: "a/A.java", Line: 1},
	}, []graph.Edge{
		{From: "a/A.java::A", To: "a/I.java::I", Kind: graph.EdgeImplements, Confidence: graph.ConfExact},
	})
	res, err := g.Traits("a/I.java::I", 2, 50)
	if err != nil {
		t.Fatalf("Traits: %v", err)
	}
	out := FormatTraits(res)
	if !strings.Contains(out, "- (none)") {
		t.Errorf("FormatTraits missing '- (none)' for empty impact:\n%s", out)
	}
}
