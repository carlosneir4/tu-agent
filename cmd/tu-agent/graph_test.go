package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written. Not parallel-safe: it swaps the process-global os.Stdout (and tests
// here also os.Chdir), so callers must not use t.Parallel().
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	runErr := fn()
	_ = w.Close()
	data, _ := io.ReadAll(r)
	return string(data), runErr
}

func TestGraphDBPath(t *testing.T) {
	if got := graphDBPath("."); got != filepath.Join(".tu-agent", "graph", "graph.db") {
		t.Errorf("graphDBPath = %q", got)
	}
}

func TestRunGraphContextEndToEnd(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Base.java": "package p;\npublic class Base {}\n",
		"Sub.java":  "package p;\npublic class Sub extends Base {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	out, err := runGraphContext("src/Base.java::Base", 2, 50)
	if err != nil {
		t.Fatalf("runGraphContext: %v", err)
	}
	if !strings.Contains(out, "Blast radius") || !strings.Contains(out, "Sub") {
		t.Errorf("context output missing blast radius / dependent Sub:\n%s", out)
	}
}

func TestRunGraphImpactResolvesAmbiguousSymbolName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// "Svc" substring-matches the Svc class, the SvcException class, and both
	// their file nodes — four hits. The resolver must still pick the exact-name
	// class and report Ctrl (which extends Svc) as a dependent.
	files := map[string]string{
		"Svc.java":          "package p;\npublic class Svc {}\n",
		"SvcException.java": "package p;\npublic class SvcException {}\n",
		"Ctrl.java":         "package p;\npublic class Ctrl extends Svc {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	out, err := runGraphImpact("Svc", 2, 50, query.SurpriseConfig{}, false)
	if err != nil {
		t.Fatalf("runGraphImpact: %v", err)
	}
	if !strings.Contains(out, "Ctrl") {
		t.Errorf("impact of ambiguous name 'Svc' missing dependent Ctrl:\n%s", out)
	}
	// "Svc" substring-matches four nodes (Svc, SvcException, and their two file
	// nodes) but only Svc matches exactly — the multi-hit exact-match path still
	// discloses how the ambiguous input was interpreted.
	if !strings.Contains(out, `resolved "Svc" →`) {
		t.Errorf("expected disclosure line for ambiguous exact-match resolution:\n%s", out)
	}
}

func TestRunGraphImpactUnknownSymbolErrors(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Svc.java"), []byte("package p;\npublic class Svc {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	_, err := runGraphImpact("Nonexistent", 2, 50, query.SurpriseConfig{}, false)
	if err == nil || !strings.Contains(err.Error(), `symbol not found: "Nonexistent"`) {
		t.Fatalf("want symbol-not-found error, got %v", err)
	}
}

func TestRunGraphImpactAmbiguousSuggests(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// Neither class is named exactly "Svc" — querying "Svc" substring-matches
	// both (and their file nodes) with no exact-name hit, so resolution must
	// fail with up to 3 "did you mean" candidates instead of silently picking one.
	files := map[string]string{
		"SvcException.java": "package p;\npublic class SvcException {}\n",
		"SvcHelper.java":    "package p;\npublic class SvcHelper {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	_, err := runGraphImpact("Svc", 2, 50, query.SurpriseConfig{}, false)
	if err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("want did-you-mean error, got %v", err)
	}
}

func TestRunGraphImpactSubstringDisclosure(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// Class name deliberately does not match the filename, so querying the
	// substring "Alph" hits exactly one node (the class) — a single non-exact
	// match, which must carry a disclosure line rather than resolve silently.
	if err := os.WriteFile(filepath.Join(src, "Container.java"), []byte("package p;\npublic class Alpha {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	out, err := runGraphImpact("Alph", 2, 50, query.SurpriseConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `resolved "Alph" →`) {
		t.Errorf("missing disclosure line; out=%q", out)
	}
}

func TestRunGraphBuildAndImpactEndToEnd(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Base.java": "package p;\npublic class Base {}\n",
		"Sub.java":  "package p;\npublic class Sub extends Base {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// NOTE: cannot use t.Parallel() — os.Chdir affects the whole process.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	out, err := runGraphImpact("src/Base.java::Base", 2, 50, query.SurpriseConfig{}, false)
	if err != nil {
		t.Fatalf("runGraphImpact: %v", err)
	}
	if !strings.Contains(out, "Sub") {
		t.Errorf("impact output missing dependent Sub:\n%s", out)
	}
}

func TestRunGraphBuildQuiet_NoopWhenNoGraph(t *testing.T) {
	root := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	out, err := captureStdout(t, func() error { return runGraphBuildQuiet("", true) })
	if err != nil {
		t.Fatalf("runGraphBuildQuiet: %v", err)
	}
	if out != "" {
		t.Errorf("quiet no-op should print nothing, got %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("quiet no-op must not create .tu-agent")
	}
}

func TestRunGraphBuildQuiet_SilentUpdateWithExistingGraph(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("bootstrap build: %v", err)
	}
	_ = os.Remove(".mcp.json")
	if err := os.WriteFile(filepath.Join(src, "B.java"), []byte("package p;\npublic class B {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runGraphBuildQuiet("", true) })
	if err != nil {
		t.Fatalf("quiet update: %v", err)
	}
	if out != "" {
		t.Errorf("quiet update should print nothing, got %q", out)
	}
	if _, statErr := os.Stat(".mcp.json"); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("quiet update must not rewrite .mcp.json")
	}
}

func TestGraphBuild_MCPOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// Default: the plugin provides the MCP server, so the binary must NOT write
	// .mcp.json (avoids a duplicate server and a hardcoded binary path).
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(".mcp.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("default build must not write .mcp.json, stat err=%v", err)
	}

	// Opt-in: --write-mcp writes it (for CLI-only users without the plugin).
	writeMCPOptIn = true
	t.Cleanup(func() { writeMCPOptIn = false })
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build --write-mcp: %v", err)
	}
	if _, err := os.Stat(".mcp.json"); err != nil {
		t.Errorf("--write-mcp must write .mcp.json: %v", err)
	}
}

func TestRunGraphImpactSurprisingOnly(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a/Caller.java": "package a;\nimport b.Base;\npublic class Caller extends Base {}\n",
		"b/Base.java":   "package b;\npublic class Base {}\n",
	}
	for name, content := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build: %v", err)
	}

	out, err := runGraphImpact("Base", 2, 50, query.SurpriseConfig{}, true)
	if err != nil {
		t.Fatalf("runGraphImpact: %v", err)
	}
	if !strings.Contains(out, "No surprising") {
		t.Errorf("surprising-only output unexpected:\n%s", out)
	}
}

func TestRunGraphBridges(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// Base <- Mid <- Leaf via extends/calls so Mid bridges; minimal Java.
	files := map[string]string{
		"Base.java": "package p;\npublic class Base { public void run(){} }\n",
		"Mid.java":  "package p;\npublic class Mid { public void go(Base b){ b.run(); } }\n",
		"Leaf.java": "package p;\npublic class Leaf { public void start(Mid m){ m.go(null); } }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := runGraphBridges(20, 100, false)
	if err != nil {
		t.Fatalf("runGraphBridges: %v", err)
	}
	// Deterministic, exit 0; either lists chokepoints or says none — must not error.
	if out == "" {
		t.Error("expected non-empty output")
	}
}

func TestRunGraphImpact_CycleMateNote(t *testing.T) {
	dir := t.TempDir()
	// Two Go files that call each other → a 2-symbol cycle.
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("pkg/a.go", "package pkg\nfunc A() { B() }\n")
	mustWrite("pkg/b.go", "package pkg\nfunc B() { A() }\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := runGraphImpact("A", 3, 50, query.SurpriseConfig{}, false)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if !strings.Contains(out, "cycle-mate") {
		t.Errorf("impact output should note cycle-mates for a symbol in a cycle; got:\n%s", out)
	}
}

func TestRunGraphCycles(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("pkg/a.go", "package pkg\nfunc A() { B() }\n")
	mustWrite("pkg/b.go", "package pkg\nfunc B() { A() }\n")
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("build: %v", err)
	}

	out, err := runGraphCycles(false)
	if err != nil {
		t.Fatalf("cycles: %v", err)
	}
	if !strings.Contains(out, "members") {
		t.Errorf("cycles output should list a multi-member component; got:\n%s", out)
	}
}

func TestRunGraphCyclesCapsMembers(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const n = 25
	for i := 0; i < n; i++ {
		next := (i + 1) % n
		mustWrite(fmt.Sprintf("pkg/f%d.go", i),
			fmt.Sprintf("package pkg\nfunc F%d() { F%d() }\n", i, next))
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("build: %v", err)
	}

	out, err := runGraphCycles(false)
	if err != nil {
		t.Fatalf("cycles: %v", err)
	}

	var membersLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "members:") {
			membersLine = l
			break
		}
	}
	if membersLine == "" {
		t.Fatalf("no members line found in:\n%s", out)
	}
	if !strings.Contains(membersLine, fmt.Sprintf("%d members", n)) {
		t.Errorf("expected real total %q in members line, got: %q", fmt.Sprintf("%d members", n), membersLine)
	}
	if !strings.Contains(membersLine, "(+5 more)") {
		t.Errorf("expected cap marker '(+5 more)' in members line, got: %q", membersLine)
	}
	idx := strings.Index(membersLine, ": ")
	if idx == -1 {
		t.Fatalf("members line missing ': ' separator: %q", membersLine)
	}
	namesPart := strings.TrimSuffix(membersLine[idx+2:], " (+5 more)")
	names := strings.Split(namesPart, ", ")
	if len(names) != 20 {
		t.Errorf("expected exactly 20 member names listed, got %d: %v", len(names), names)
	}
}

func TestGraphImpactRelatedKnowledge(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Base.java": "package p;\npublic class Base {}\n",
		"Sub.java":  "package p;\npublic class Sub extends Base {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build: %v", err)
	}

	g, err := loadQueryGraph()
	if err != nil {
		t.Fatal(err)
	}
	subID, _, err := resolveTargetChecked(g, "Sub")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	obs, _ := ms.Upsert("gotcha/sub", "Sub has a subtle invariant", memory.UpsertOpts{})
	if _, err := ms.Relate(obs.ID, subID, "related"); err != nil {
		t.Fatal(err)
	}
	_ = ms.Close()

	out := relatedKnowledgeSection("Base", []string{subID})
	if !strings.Contains(out, "gotcha/sub") {
		t.Errorf("related knowledge section missing the linked observation:\n%s", out)
	}
	if relatedKnowledgeSection("Base", []string{"no/such::Node"}) != "" {
		t.Error("unrelated node set should yield empty section")
	}
}

func TestRelatedKnowledge_MarksAuto(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	o, err := ms.Upsert("gotcha/x", "OrderService trap", memory.UpsertOpts{Type: "gotcha"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Relate(o.ID, "src/OrderService.java::OrderService", "documents_auto"); err != nil {
		t.Fatal(err)
	}
	_ = ms.Close()

	got := relatedKnowledgeSection("src/OrderService.java::OrderService", nil)
	if !strings.Contains(got, "gotcha/x") {
		t.Fatalf("expected the linked note, got:\n%s", got)
	}
	if !strings.Contains(got, "~auto") {
		t.Fatalf("auto link must be marked ~auto, got:\n%s", got)
	}
}

func TestGraphImpactRelatedKnowledgeMarksStale(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Base.java": "package p;\npublic class Base {}\n",
		"Sub.java":  "package p;\npublic class Sub extends Base {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("build: %v", err)
	}

	g, err := loadQueryGraph()
	if err != nil {
		t.Fatal(err)
	}
	subID, _, err := resolveTargetChecked(g, "Sub")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	obs, err := ms.Upsert("gotcha/stale-sub", "Sub note whose other link has vanished", memory.UpsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	// Found by the impact query via this link to a real, live node...
	if _, err := ms.Relate(obs.ID, subID, "related"); err != nil {
		t.Fatal(err)
	}
	// ...but flagged stale because it also links to a node that no longer exists.
	if _, err := ms.Relate(obs.ID, "src/Ghost.java::Ghost", "documents"); err != nil {
		t.Fatal(err)
	}
	_ = ms.Close()

	out, err := runGraphImpact("Base", 3, 50, query.SurpriseConfig{}, false)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if !strings.Contains(out, "gotcha/stale-sub") {
		t.Fatalf("Related knowledge block must list the linked observation:\n%s", out)
	}
	if !strings.Contains(out, "possibly stale") {
		t.Errorf("Related knowledge block must mark a stale-linked note:\n%s", out)
	}
}
