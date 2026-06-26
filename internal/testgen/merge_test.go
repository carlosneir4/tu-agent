package testgen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/query"
)

func goTarget() (Target, *query.Graph) {
	nodes := []graph.Node{{ID: "store.go::Save", Kind: graph.KindFunction, Name: "Store.Save",
		Path: "store.go", Line: 1, EndLine: 2, Language: "go", Exported: true}}
	return TargetFromNode(nodes[0]), query.NewGraph(nodes, nil)
}

// TestGenerateWritesWholeFileWhenAbsent: no conventional file → the generated
// file is written verbatim and the scoped run passes.
func TestGenerateWritesWholeFileWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte("package s\n\nfunc Save(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module s\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tgt, g := goTarget()
	canned := "```go\npackage s\n\nimport \"testing\"\n\nfunc TestStoreSave_Gen(t *testing.T){}\n```"
	p := Pipeline{Graph: g, Adapter: &GoAdapter{}, Provider: &fakeProvider{responses: []string{canned}},
		Run: func(context.Context, string, []string, time.Duration) (string, error) { return "ok", nil }}
	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 0})
	if err != nil || !res.Passed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "store_test.go"))
	if !strings.Contains(string(data), "TestStoreSave_Gen") {
		t.Fatalf("missing generated func:\n%s", data)
	}
}

func TestMergeGoCleanMerge(t *testing.T) {
	tgt, _ := goTarget() // Name "Store.Save" → prefix TestStoreSave_Gen
	existing := "package s\n\nimport \"testing\"\n\nfunc TestExisting(t *testing.T) { _ = 1 }\n"
	generated := "package s\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestStoreSave_Gen(t *testing.T) { _ = strings.ToUpper(\"x\") }\n"
	out, err := mergeGo(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func TestExisting", "func TestStoreSave_Gen", "\"strings\"", "\"testing\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Idempotent: merging again replaces, never duplicates.
	out2, err := mergeGo(out, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out2, "func TestStoreSave_Gen") != 1 {
		t.Fatalf("generated func duplicated on re-merge:\n%s", out2)
	}
	if strings.Count(out2, "func TestExisting") != 1 {
		t.Fatalf("hand-written func duplicated:\n%s", out2)
	}
}

func TestMergeGoPreservesHandwrittenNearMarker(t *testing.T) {
	tgt, _ := goTarget() // marker TestStoreSave_Gen
	// Hand-written func shares the marker as a prefix but is NOT a generated func.
	existing := "package s\n\nimport \"testing\"\n\nfunc TestStoreSave_GenuineEdge(t *testing.T) { _ = 1 }\n"
	generated := "package s\n\nimport \"testing\"\n\nfunc TestStoreSave_Gen(t *testing.T) { _ = 1 }\n"
	out, err := mergeGo(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "func TestStoreSave_GenuineEdge") {
		t.Fatalf("hand-written near-marker func was dropped:\n%s", out)
	}
	if strings.Count(out, "func TestStoreSave_Gen(") != 1 {
		t.Fatalf("generated func count wrong:\n%s", out)
	}
}

func TestMergePython(t *testing.T) {
	tgt := Target{Name: "Store.Save", Path: "store.py", Language: "python"}
	existing := "import pytest\n\n\ndef test_existing():\n    assert True\n"
	generated := "import pytest\nfrom store import Store\n\n# tu-agent:gen:start\ndef test_store_save_gen():\n    assert Store().save() is None\n# tu-agent:gen:end\n"
	out, err := mergePython(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"def test_existing", "def test_store_save_gen", "from store import Store", "tu-agent:gen:start"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "import pytest") != 1 {
		t.Fatalf("import not deduped:\n%s", out)
	}
	out2, err := mergePython(out, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out2, "def test_store_save_gen") != 1 {
		t.Fatalf("gen func duplicated on re-merge:\n%s", out2)
	}
}

func TestMergeTS(t *testing.T) {
	tgt := Target{Name: "Store.save", Path: "store.ts", Language: "typescript"}
	existing := "import { describe, it, expect } from \"vitest\";\n\ndescribe(\"hand\", () => {\n  it(\"keeps\", () => { expect(1).toBe(1); });\n});\n"
	generated := "import { describe, it, expect } from \"vitest\";\nimport { Store } from \"./store\";\n\n// tu-agent:gen:start\ndescribe(\"Store.save (gen)\", () => {\n  it(\"saves\", () => { expect(new Store().save()).toBeUndefined(); });\n});\n// tu-agent:gen:end\n"
	out, err := mergeTS(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`describe("hand"`, `describe("Store.save (gen)"`, `import { Store } from "./store"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, `from "vitest"`) != 1 {
		t.Fatalf("vitest import not deduped:\n%s", out)
	}
	out2, err := mergeTS(out, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out2, `describe("Store.save (gen)"`) != 1 {
		t.Fatalf("gen describe duplicated on re-merge:\n%s", out2)
	}
}

func TestMergeJava(t *testing.T) {
	tgt := Target{Name: "OrderService.placeOrder", Path: "src/main/java/OrderService.java", Language: "java"}
	existing := "package o;\n\nimport org.junit.jupiter.api.Test;\n\npublic class OrderServiceTest {\n    @Test void existing() { }\n}\n"
	generated := "package o;\n\nimport org.junit.jupiter.api.Test;\nimport static org.junit.jupiter.api.Assertions.*;\n\npublic class OrderServiceTest {\n    // tu-agent:gen:start\n    @Test void placeOrderGen() { assertTrue(true); }\n    // tu-agent:gen:end\n}\n"
	out, err := mergeJava(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"void existing()", "void placeOrderGen()", "import static org.junit.jupiter.api.Assertions.*;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "import org.junit.jupiter.api.Test;") != 1 {
		t.Fatalf("Test import not deduped:\n%s", out)
	}
	if strings.Count(out, "public class OrderServiceTest") != 1 {
		t.Fatalf("class duplicated:\n%s", out)
	}
	out2, err := mergeJava(out, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out2, "void placeOrderGen()") != 1 {
		t.Fatalf("gen method duplicated on re-merge:\n%s", out2)
	}
}

// TestGenerateFallbackPreservesHandwritten: an existing, UNMERGEABLE file must
// never be clobbered — the hand-written func survives and the generated test is
// appended commented-out under a FIXME.
func TestGenerateFallbackPreservesHandwritten(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte("package s\n\nfunc Save(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module s\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unparseable Go → mergeGo returns errUnmergeable on every attempt.
	handwritten := "this is not valid go !!! func TestExisting(){}\n"
	if err := os.WriteFile(filepath.Join(root, "store_test.go"), []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}
	tgt, g := goTarget()
	canned := "```go\npackage s\n\nimport \"testing\"\n\nfunc TestStoreSave_Gen(t *testing.T){}\n```"
	p := Pipeline{Graph: g, Adapter: &GoAdapter{}, Provider: &fakeProvider{responses: []string{canned, canned, canned}},
		Run: func(context.Context, string, []string, time.Duration) (string, error) { return "ok", nil }}
	res, _ := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if !res.FIXME {
		t.Fatalf("want FIXME fallback, res=%+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(root, "store_test.go"))
	got := string(data)
	if !strings.Contains(got, "func TestExisting") {
		t.Fatalf("hand-written test lost:\n%s", got)
	}
	if !strings.Contains(got, "FIXME") || !strings.Contains(got, "// func TestStoreSave_Gen") {
		t.Fatalf("generated not appended commented-out:\n%s", got)
	}
}
