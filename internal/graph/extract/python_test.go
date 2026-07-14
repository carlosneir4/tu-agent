package extract

import (
	"slices"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

const pySrc = `from billing.base import Base
import os

class Invoice(Base):
    def total(self):
        return compute()

def compute():
    return 1
`

func TestParsePython_declarationsAndEdges(t *testing.T) {
	f, err := ParsePython("billing/invoice.py", []byte(pySrc))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	if f.Meta.Language != "python" || f.Meta.Package != "billing.invoice" {
		t.Fatalf("meta = %+v, want language=python package=billing.invoice", f.Meta)
	}
	want := map[string]graph.NodeKind{
		"billing/invoice.py":                graph.KindFile,
		"billing/invoice.py::Invoice":       graph.KindClass,
		"billing/invoice.py::Invoice.total": graph.KindFunction,
		"billing/invoice.py::compute":       graph.KindFunction,
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
	assertRef(t, f, "billing/invoice.py::Invoice", graph.EdgeExtends, "Base")
	assertRef(t, f, "billing/invoice.py::Invoice.total", graph.EdgeCalls, "compute")
	// `from billing.base import Base` normalizes to a resolvable FQN import.
	assertImport(t, f, "billing.base.Base")
}

func TestParsePython_testFile(t *testing.T) {
	f, err := ParsePython("tests/test_invoice.py", []byte("def test_total():\n    pass\n"))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	found := false
	for _, n := range f.Nodes {
		if n.Name == "test_total" {
			found = true
			if n.Kind != graph.KindTest {
				t.Errorf("test_total kind = %q, want test", n.Kind)
			}
		}
	}
	if !found {
		t.Error("node test_total not found")
	}
}

func assertImport(t *testing.T, f *FileFacts, want string) {
	t.Helper()
	if !slices.Contains(f.Meta.Imports, want) {
		t.Errorf("missing import %q in %v", want, f.Meta.Imports)
	}
}

const signaturePySrc = `class InvoiceService:
    def process(self, invoice: Invoice,
                force: bool = False) -> int:
        return 0

def helper(x):
    return x
`

func TestParsePythonExtractsSignatures(t *testing.T) {
	facts, err := ParsePython("billing/svc.py", []byte(signaturePySrc))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	byID := map[string]graph.Node{}
	for _, n := range facts.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id, params, ret string
	}{
		{"billing/svc.py::InvoiceService.process", "(self, invoice: Invoice, force: bool = False)", "int"},
		{"billing/svc.py::helper", "(x)", ""},
	}
	for _, tc := range tests {
		n, ok := byID[tc.id]
		if !ok {
			t.Errorf("node %s missing", tc.id)
			continue
		}
		if n.Params != tc.params || n.ReturnType != tc.ret {
			t.Errorf("%s signature = %q / %q, want %q / %q", tc.id, n.Params, n.ReturnType, tc.params, tc.ret)
		}
	}
}

func TestParsePython_exportedFlag(t *testing.T) {
	src := `class Svc:
    def method(self):
        return 1

    def _hidden(self):
        return 2

def public_fn():
    return 3

def _private_fn():
    return 4
`
	f, err := ParsePython("pkg/svc.py", []byte(src))
	if err != nil {
		t.Fatalf("ParsePython: %v", err)
	}
	want := map[string]bool{
		"pkg/svc.py::Svc.method":  true,
		"pkg/svc.py::Svc._hidden": false,
		"pkg/svc.py::public_fn":   true,
		"pkg/svc.py::_private_fn": false,
	}
	got := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Kind == graph.KindFunction {
			got[n.ID] = n.Exported
		}
	}
	for id, exp := range want {
		if got[id] != exp {
			t.Errorf("node %q Exported = %v, want %v", id, got[id], exp)
		}
	}
}
