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

// TestMergeGoPreservesImportAlias: a generated file importing a package under
// an alias (`x "strings"`) must keep that alias in the merged output — keying
// the import union by bare Path.Value alone (the pre-fix behavior) collapses
// it to a bare `"strings"` import while the generated func body still calls
// `x.Contains`, producing a file that does not compile.
func TestMergeGoPreservesImportAlias(t *testing.T) {
	tgt, _ := goTarget() // marker "TestStoreSave_Gen"
	existing := "package s\n\nimport \"testing\"\n\nfunc TestExisting(t *testing.T) { _ = 1 }\n"
	generated := "package s\n\nimport (\n\tx \"strings\"\n\t\"testing\"\n)\n\nfunc TestStoreSave_Gen(t *testing.T) { _ = x.Contains(\"a\", \"b\") }\n"
	out, err := mergeGo(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `x "strings"`) {
		t.Fatalf("aliased import was dropped:\n%s", out)
	}
	if strings.Contains(out, "\t\"strings\"\n") {
		t.Fatalf("import collapsed to bare path, losing the alias:\n%s", out)
	}
	if !strings.Contains(out, "x.Contains") {
		t.Fatalf("generated func body missing:\n%s", out)
	}
}

// TestMergeGoPreservesDotImport: same as above but for a dot import, which
// must also survive as `.` rather than being flattened to a bare path.
func TestMergeGoPreservesDotImport(t *testing.T) {
	tgt, _ := goTarget() // marker "TestStoreSave_Gen"
	existing := "package s\n\nimport \"testing\"\n\nfunc TestExisting(t *testing.T) { _ = 1 }\n"
	generated := "package s\n\nimport (\n\t. \"strings\"\n\t\"testing\"\n)\n\nfunc TestStoreSave_Gen(t *testing.T) { _ = Contains(\"a\", \"b\") }\n"
	out, err := mergeGo(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `. "strings"`) {
		t.Fatalf("dot import was dropped:\n%s", out)
	}
	if strings.Contains(out, "\t\"strings\"\n") {
		t.Fatalf("import collapsed to bare path, losing the dot qualifier:\n%s", out)
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

// TestMergeGoPreservesCommentsAndBuildTags: mergeGo must splice by byte range
// over the original text — file-header comments, //go:build tags, doc comments,
// and trailing comments on hand-written code must survive byte-identical. The
// marker is "TestStoreSave_Gen" (goGenPrefix(goTarget()'s Target)); generated
// func names use the marker+"_"+suffix form recognized by isGenFuncName, same
// as every other test in this file.
func TestMergeGoPreservesCommentsAndBuildTags(t *testing.T) {
	tgt, _ := goTarget() // marker "TestStoreSave_Gen"
	existing := `//go:build integration

// Package-level doc comment that must survive.
package s

import "testing"

// handHelper has a doc comment.
func handHelper() int { return 1 } // trailing comment survives too

func TestStoreSave_Gen_Old(t *testing.T) { t.Skip("old generated") }

// TestHand is hand-written and untouchable.
func TestHand(t *testing.T) {
	if handHelper() != 1 {
		t.Fatal("nope")
	}
}
`
	generated := "package s\n\nimport \"testing\"\n\nfunc TestStoreSave_Gen_New(t *testing.T) { t.Fatal(\"red\") }\n"
	out, err := mergeGo(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"//go:build integration", "// Package-level doc comment that must survive.",
		"// handHelper has a doc comment.", "// trailing comment survives too",
		"// TestHand is hand-written and untouchable.", "TestStoreSave_Gen_New"} {
		if !strings.Contains(out, want) {
			t.Errorf("merge lost %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TestStoreSave_Gen_Old") {
		t.Errorf("old generated func not replaced:\n%s", out)
	}
}

// TestMergePython pins back-compat: the generated fixture uses the LEGACY
// bare "# tu-agent:gen:start"/"# tu-agent:gen:end" pair (no per-target
// suffix) — splitRegion's fallback must still treat it as this target's
// region when no suffixed pair exists yet, and the re-merge must re-emit
// (accept) the same bare form without erroring.
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

// TestMergeTS uses the per-target suffixed sentinel form ("Store_save" from
// sentinelKey), the normal path for newly generated content.
func TestMergeTS(t *testing.T) {
	tgt := Target{Name: "Store.save", Path: "store.ts", Language: "typescript"}
	existing := "import { describe, it, expect } from \"vitest\";\n\ndescribe(\"hand\", () => {\n  it(\"keeps\", () => { expect(1).toBe(1); });\n});\n"
	generated := "import { describe, it, expect } from \"vitest\";\nimport { Store } from \"./store\";\n\n// tu-agent:gen:start:Store_save\ndescribe(\"Store.save (gen)\", () => {\n  it(\"saves\", () => { expect(new Store().save()).toBeUndefined(); });\n});\n// tu-agent:gen:end:Store_save\n"
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

// TestMergeJava uses the per-target suffixed sentinel form ("placeOrderGen"
// from javaGenPrefix, via sentinelKey), the normal path for newly generated
// content.
func TestMergeJava(t *testing.T) {
	tgt := Target{Name: "OrderService.placeOrder", Path: "src/main/java/OrderService.java", Language: "java"}
	existing := "package o;\n\nimport org.junit.jupiter.api.Test;\n\npublic class OrderServiceTest {\n    @Test void existing() { }\n}\n"
	generated := "package o;\n\nimport org.junit.jupiter.api.Test;\nimport static org.junit.jupiter.api.Assertions.*;\n\npublic class OrderServiceTest {\n    // tu-agent:gen:start:placeOrderGen\n    @Test void placeOrderGen() { assertTrue(true); }\n    // tu-agent:gen:end:placeOrderGen\n}\n"
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

// TestMergeSentinelsCrossTargetPreserved: the old global sentinel pair let
// generating tests for one target delete a DIFFERENT target's region in the
// same file. With per-target suffixed sentinels ("placeOrderGen" vs
// "cancelOrderGen", both from javaGenPrefix via sentinelKey), merging target
// B's generated tests must never touch target A's already-merged region —
// the output must contain BOTH.
func TestMergeSentinelsCrossTargetPreserved(t *testing.T) {
	tgtB := Target{Name: "OrderService.cancelOrder", Path: "src/main/java/OrderService.java", Language: "java"}
	// existing already holds target A's ("placeOrder") merged region.
	existing := "package o;\n\n" +
		"import org.junit.jupiter.api.Test;\n\n" +
		"public class OrderServiceTest {\n" +
		"    @Test void existing() { }\n" +
		"    // tu-agent:gen:start:placeOrderGen\n" +
		"    @Test void placeOrderGen() { }\n" +
		"    // tu-agent:gen:end:placeOrderGen\n" +
		"}\n"
	generatedB := "package o;\n\n" +
		"import org.junit.jupiter.api.Test;\n\n" +
		"public class OrderServiceTest {\n" +
		"    // tu-agent:gen:start:cancelOrderGen\n" +
		"    @Test void cancelOrderGen() { }\n" +
		"    // tu-agent:gen:end:cancelOrderGen\n" +
		"}\n"
	out, err := mergeJava(existing, generatedB, tgtB)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"void existing()",
		"void placeOrderGen()", // target A's region — must survive generating B
		"void cancelOrderGen()",
		"tu-agent:gen:start:placeOrderGen",
		"tu-agent:gen:start:cancelOrderGen",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-target clobber: missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "public class OrderServiceTest") != 1 {
		t.Fatalf("class duplicated:\n%s", out)
	}
}

// TestMergeSentinelsIgnoresCommentedFixmeBlock: a prior fixmeAppend left a
// neutralized (commented-out, sentinel-mangled) dead region ABOVE a
// hand-written method, which sits above the LIVE generated region for the
// same target. Re-merging that target must replace only the live region —
// the hand-written method and the dead FIXME block must be untouched.
// Without exact-line matching and neutralization, the old strings.Contains
// scan would pair the dead block's mangled start with the live region's end
// (or vice-versa) and delete everything in between, including the
// hand-written method.
func TestMergeSentinelsIgnoresCommentedFixmeBlock(t *testing.T) {
	tgt := Target{Name: "OrderService.placeOrder", Path: "src/main/java/OrderService.java", Language: "java"}
	// A previously failed generation, neutralized + commented by fixmeAppend.
	failedGen := "// tu-agent:gen:start:placeOrderGen\n@Test void placeOrderGen() { fail(\"boom\"); }\n// tu-agent:gen:end:placeOrderGen\n"
	fixmeBlock := fixmeAppend("", failedGen, "java", 2)
	existing := "package o;\n\n" +
		"import org.junit.jupiter.api.Test;\n\n" +
		"public class OrderServiceTest {\n" +
		fixmeBlock +
		"    @Test void handWritten() { }\n" +
		"    // tu-agent:gen:start:placeOrderGen\n" +
		"    @Test void placeOrderGenOld() { }\n" +
		"    // tu-agent:gen:end:placeOrderGen\n" +
		"}\n"
	generated := "package o;\n\n" +
		"import org.junit.jupiter.api.Test;\n\n" +
		"public class OrderServiceTest {\n" +
		"    // tu-agent:gen:start:placeOrderGen\n" +
		"    @Test void placeOrderGenNew() { }\n" +
		"    // tu-agent:gen:end:placeOrderGen\n" +
		"}\n"
	out, err := mergeJava(existing, generated, tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "void handWritten()") {
		t.Fatalf("hand-written method between the FIXME block and the live region was deleted (spanning delete):\n%s", out)
	}
	if strings.Count(out, "void placeOrderGenNew()") != 1 {
		t.Fatalf("new gen method missing or duplicated:\n%s", out)
	}
	if strings.Contains(out, "void placeOrderGenOld()") {
		t.Fatalf("old live region for this target was not replaced:\n%s", out)
	}
	if !strings.Contains(out, "FIXME") || !strings.Contains(out, "placeOrderGen() { fail(\"boom\"); }") {
		t.Fatalf("dead FIXME block was disturbed by the re-merge:\n%s", out)
	}
}

// TestSplitRegionExactLineMatch: a line that merely CONTAINS the sentinel
// text inside a string literal (or any other prose) must not open a region —
// matching is by the FULL trimmed line, not a substring scan.
func TestSplitRegionExactLineMatch(t *testing.T) {
	tgt := Target{Name: "OrderService.placeOrder", Path: "src/main/java/OrderService.java", Language: "java"}
	src := "String s = \"tu-agent:gen:start:placeOrderGen\";\n" +
		"// tu-agent:gen:start:placeOrderGen\n" +
		"@Test void placeOrderGen() { }\n" +
		"// tu-agent:gen:end:placeOrderGen\n"
	before, region, _, ok := splitRegion(src, "//", tgt)
	if !ok {
		t.Fatalf("expected the real comment-line sentinels to match:\n%s", src)
	}
	if strings.Contains(before, "@Test void placeOrderGen") {
		t.Fatalf("region opened on the string-literal line, not the real sentinel comment:\nbefore=%q", before)
	}
	if !strings.Contains(before, `String s = "tu-agent:gen:start:placeOrderGen";`) {
		t.Fatalf("string-literal line should remain in 'before' untouched:\nbefore=%q", before)
	}
	if !strings.Contains(region, "@Test void placeOrderGen") {
		t.Fatalf("region missing the generated method:\nregion=%q", region)
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
