package testgen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/query"
)

const cannedCalcTest = "```go\npackage calcfixture\n\nimport \"testing\"\n\nfunc TestAdd_Gen(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2,3) = %d, want 5\", got)\n\t}\n}\n```"

// TestGenerateEndToEndGo runs the real pipeline — real file writes, real
// `go test` subprocess — with only the provider mocked.
func TestGenerateEndToEndGo(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the go toolchain")
	}
	// Copy the fixture to a temp dir so the repo tree stays clean.
	root := t.TempDir()
	for _, f := range []string{"go.mod", "calc.go"} {
		data, err := os.ReadFile(filepath.Join("testdata", "gofixture", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nodes := []graph.Node{
		{ID: "calc.go::Add", Kind: graph.KindFunction, Name: "Add", Path: "calc.go",
			Line: 4, EndLine: 6, Language: "go", Params: "(a, b int)", ReturnType: "int", Exported: true},
	}
	g := query.NewGraph(nodes, nil)
	p := Pipeline{
		Graph:    g,
		Adapter:  &GoAdapter{},
		Provider: &fakeProvider{responses: []string{cannedCalcTest}},
		// Run nil → real ExecRunner
	}

	res, err := p.Generate(context.Background(), TargetFromNode(nodes[0]), Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts != 1 || res.TestPath != "calc_test.go" {
		t.Fatalf("res = %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(root, "calc_test.go"))
	if err != nil || !strings.Contains(string(data), "func TestAdd") {
		t.Fatalf("file: %v / %s", err, data)
	}
}
