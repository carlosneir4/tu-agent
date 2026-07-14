package extract

import (
	"slices"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

const tsSrc = `import { Base } from "./base";

export function slugify(text: string): string {
  return text.trim();
}

function helper(x) {
  return x;
}

export const truncate = (text: string, limit: number): string => {
  return helper(text);
};

export class InvoiceService extends Base implements Printable {
  constructor() {
    super();
  }
  total(invoice: Invoice): number {
    return this.compute(invoice);
  }
  private compute(invoice: Invoice): number {
    return 0;
  }
}
`

func TestParseTypeScript_declarations(t *testing.T) {
	f, err := ParseTypeScript("src/billing/invoice.ts", []byte(tsSrc))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	if f.Meta.Language != "typescript" || f.Meta.Package != "src.billing.invoice" {
		t.Fatalf("meta = %+v, want language=typescript package=src.billing.invoice", f.Meta)
	}
	want := map[string]graph.NodeKind{
		"src/billing/invoice.ts":                         graph.KindFile,
		"src/billing/invoice.ts::slugify":                graph.KindFunction,
		"src/billing/invoice.ts::helper":                 graph.KindFunction,
		"src/billing/invoice.ts::truncate":               graph.KindFunction,
		"src/billing/invoice.ts::InvoiceService":         graph.KindClass,
		"src/billing/invoice.ts::InvoiceService.total":   graph.KindFunction,
		"src/billing/invoice.ts::InvoiceService.compute": graph.KindFunction,
	}
	got := map[string]graph.NodeKind{}
	for _, n := range f.Nodes {
		got[n.ID] = n.Kind
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("node %q kind = %q, want %q", id, got[id], kind)
		}
	}
}

func TestParseTypeScript_signaturesAndExported(t *testing.T) {
	f, err := ParseTypeScript("src/billing/invoice.ts", []byte(tsSrc))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	byID := map[string]graph.Node{}
	for _, n := range f.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id, params, ret string
		exported        bool
	}{
		{"src/billing/invoice.ts::slugify", "(text: string)", "string", true},
		{"src/billing/invoice.ts::helper", "(x)", "", false},
		{"src/billing/invoice.ts::truncate", "(text: string, limit: number)", "string", true},
		{"src/billing/invoice.ts::InvoiceService.total", "(invoice: Invoice)", "number", true},
		{"src/billing/invoice.ts::InvoiceService.compute", "(invoice: Invoice)", "number", false},
		{"src/billing/invoice.ts::InvoiceService.constructor", "()", "", false},
	}
	for _, tc := range tests {
		n, ok := byID[tc.id]
		if !ok {
			t.Errorf("node %s missing", tc.id)
			continue
		}
		if n.Params != tc.params || n.ReturnType != tc.ret || n.Exported != tc.exported {
			t.Errorf("%s = params %q ret %q exported %v, want %q %q %v",
				tc.id, n.Params, n.ReturnType, n.Exported, tc.params, tc.ret, tc.exported)
		}
	}
}

func TestParseTypeScript_tsxGrammar(t *testing.T) {
	src := `export function Button({ label }: { label: string }) {
  return <button>{label}</button>;
}
`
	f, err := ParseTypeScript("src/ui/Button.tsx", []byte(src))
	if err != nil {
		t.Fatalf("ParseTypeScript(.tsx): %v", err)
	}
	if f.Meta.Language != "typescript" || f.Meta.Package != "src.ui.Button" {
		t.Fatalf("meta = %+v", f.Meta)
	}
	for _, n := range f.Nodes {
		if n.ID == "src/ui/Button.tsx::Button" {
			if !n.Exported || n.Kind != graph.KindFunction {
				t.Fatalf("Button node = %+v, want exported function", n)
			}
			return
		}
	}
	t.Fatal("node src/ui/Button.tsx::Button not found")
}

func TestParseTypeScript_testFile(t *testing.T) {
	src := "export function checkTotal(): number {\n  return 1;\n}\n"
	f, err := ParseTypeScript("src/__tests__/invoice.test.ts", []byte(src))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	for _, n := range f.Nodes {
		if n.Name == "checkTotal" && n.Kind != graph.KindTest {
			t.Errorf("checkTotal kind = %q, want test", n.Kind)
		}
	}
}

func TestIsTSTestFile(t *testing.T) {
	cases := map[string]bool{
		"src/slug.test.ts":      true,
		"src/slug.spec.tsx":     true,
		"src/__tests__/slug.ts": true,
		"src/slug.ts":           false,
		"src/latest.ts":         false,
	}
	for path, want := range cases {
		if got := isTSTestFile(path); got != want {
			t.Errorf("isTSTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

const tsRefSrc = `import { Base, Other } from "./base";
import * as fmt from "../util/fmt";
import slug from "./slug";
import "./side";
import { chunk } from "lodash";

export class InvoiceService extends Base implements Printable {
  total(invoice: Invoice): number {
    return compute(invoice);
  }
}

export function compute(invoice: Invoice): number {
  return fmt.round(invoice.amount);
}
`

func TestParseTypeScript_imports(t *testing.T) {
	f, err := ParseTypeScript("src/billing/invoice.ts", []byte(tsRefSrc))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	for _, want := range []string{
		"src.billing.base.Base",
		"src.billing.base.Other",
		"src.util.fmt.*",
		"src.billing.slug.*",
		"src.billing.side.*",
	} {
		assertImport(t, f, want)
	}
	for _, imp := range f.Meta.Imports {
		if strings.Contains(imp, "lodash") {
			t.Errorf("bare specifier leaked into imports: %q", imp)
		}
	}
}

func TestParseTypeScript_refs(t *testing.T) {
	f, err := ParseTypeScript("src/billing/invoice.ts", []byte(tsRefSrc))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	assertRef(t, f, "src/billing/invoice.ts::InvoiceService", graph.EdgeExtends, "Base")
	assertRef(t, f, "src/billing/invoice.ts::InvoiceService", graph.EdgeImplements, "Printable")
	assertRef(t, f, "src/billing/invoice.ts::InvoiceService.total", graph.EdgeCalls, "compute")
	assertRef(t, f, "src/billing/invoice.ts::compute", graph.EdgeCalls, "round")
}

func TestParseTypeScript_testCallbackCalls(t *testing.T) {
	src := `import { describe, it, expect } from "vitest";
import { slugify } from "./slug";

describe("slugify", () => {
  it("joins words", () => {
    expect(slugify("a b")).toBe("a-b");
  });
});
`
	f, err := ParseTypeScript("src/slug.test.ts", []byte(src))
	if err != nil {
		t.Fatalf("ParseTypeScript: %v", err)
	}
	assertRef(t, f, "src/slug.test.ts", graph.EdgeCalls, "slugify")
}

func TestParseTypeScript_GraphQL(t *testing.T) {
	src := []byte("import x from 'y'\n" +
		"export const frag = graphql`fragment Card_item on Item {\n id\n ...Sub_fields\n}`\n")
	f, err := ParseTypeScript("src/card.tsx", src)
	if err != nil {
		t.Fatal(err)
	}
	var frag *graph.Node
	for i := range f.Nodes {
		if f.Nodes[i].Kind == graph.KindGraphQLFragment {
			frag = &f.Nodes[i]
		}
	}
	if frag == nil || frag.Name != "Card_item" {
		t.Fatalf("no GraphQL fragment node in FileFacts: %+v", f.Nodes)
	}
	var sawContains, sawSpread bool
	for _, e := range f.Contains {
		if e.To == "src/card.tsx::gql::Card_item" && e.Kind == graph.EdgeContains && e.Confidence == graph.ConfExact {
			sawContains = true
		}
	}
	for _, r := range f.Refs {
		if r.Kind == graph.EdgeSpreads && r.Name == "Sub_fields" {
			sawSpread = true
		}
	}
	if !sawContains {
		t.Error("missing contains edge file -> fragment")
	}
	if !sawSpread {
		t.Error("missing spreads ref for Sub_fields")
	}
}

func TestTSTestSources(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"src/slug.test.ts", []string{"src/slug.ts"}},
		{"src/Button.spec.tsx", []string{"src/Button.tsx"}},
		{"src/__tests__/slug.test.ts", []string{"src/__tests__/slug.ts", "src/slug.ts"}},
		{"src/__tests__/slug.ts", []string{"src/slug.ts"}},
		{"src/slug.ts", nil},
	}
	for _, tc := range tests {
		if got := tsTestSources(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("tsTestSources(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
