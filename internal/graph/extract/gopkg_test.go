package extract

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

const goSrc = `package billing

import "github.com/acme/money"

type Service struct {
	Base
	rate money.Rate
}

type Base struct{}

func (s *Service) Calculate() int {
	return helper()
}

func helper() int { return 1 }
`

// assertRef checks that f.Refs contains an edge from -> name of the given kind.
// Shared by the Go and Python golden tests.
func assertRef(t *testing.T, f *FileFacts, from string, kind graph.EdgeKind, name string) {
	t.Helper()
	for _, r := range f.Refs {
		if r.FromID == from && r.Kind == kind && r.Name == name {
			return
		}
	}
	t.Errorf("missing ref %s -%s-> %s in %+v", from, kind, name, f.Refs)
}

func TestParseGo_declarationsAndEdges(t *testing.T) {
	f, err := ParseGo("billing/service.go", []byte(goSrc))
	if err != nil {
		t.Fatalf("ParseGo: %v", err)
	}
	if f.Meta.Language != "go" || f.Meta.Package != "billing" {
		t.Fatalf("meta = %+v, want language=go package=billing", f.Meta)
	}
	want := map[string]graph.NodeKind{
		"billing/service.go":                    graph.KindFile,
		"billing/service.go::Service":           graph.KindClass,
		"billing/service.go::Base":              graph.KindClass,
		"billing/service.go::Service.Calculate": graph.KindFunction,
		"billing/service.go::helper":            graph.KindFunction,
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
	assertRef(t, f, "billing/service.go::Service", graph.EdgeExtends, "Base")
	assertRef(t, f, "billing/service.go::Service.Calculate", graph.EdgeCalls, "helper")
}

func TestParseGo_testFile(t *testing.T) {
	f, err := ParseGo("billing/service_test.go", []byte("package billing\nfunc TestCalc(t *T){}\n"))
	if err != nil {
		t.Fatalf("ParseGo: %v", err)
	}
	for _, n := range f.Nodes {
		if n.Name == "TestCalc" && n.Kind != graph.KindTest {
			t.Errorf("TestCalc kind = %q, want test", n.Kind)
		}
	}
}

const signatureGoSrc = `package billing

func Process(invoice Invoice,
	force bool) (int, error) {
	return 0, nil
}

func Run() {}

type Service struct{}

func (s *Service) Close() error { return nil }
`

func TestParseGoExtractsSignatures(t *testing.T) {
	facts, err := ParseGo("billing/svc.go", []byte(signatureGoSrc))
	if err != nil {
		t.Fatalf("ParseGo: %v", err)
	}
	byID := map[string]graph.Node{}
	for _, n := range facts.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id, params, ret string
	}{
		{"billing/svc.go::Process", "(invoice Invoice, force bool)", "(int, error)"},
		{"billing/svc.go::Run", "()", ""},
		{"billing/svc.go::Service.Close", "()", "error"},
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

func TestParseGo_exportedFlag(t *testing.T) {
	src := `package p

type Svc struct{}

func Public() int { return 1 }
func private() int { return 2 }
func (s *Svc) Method() int { return 3 }
func (s *Svc) hidden() int { return 4 }
`
	f, err := ParseGo("p/svc.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseGo: %v", err)
	}
	want := map[string]bool{
		"p/svc.go::Public":     true,
		"p/svc.go::private":    false,
		"p/svc.go::Svc.Method": true,
		"p/svc.go::Svc.hidden": false,
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
