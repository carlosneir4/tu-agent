package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph/query"
	"github.com/tu/tu-agent/internal/memory"
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
	if got := graphDBPath("."); got != filepath.Join(".tu-agent", "graph.db") {
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
	subID := resolveTarget(g, "Sub")
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
