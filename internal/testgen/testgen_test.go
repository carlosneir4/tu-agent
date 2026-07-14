package testgen

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/provider"
)

func TestBuildScaffold(t *testing.T) {
	g, root, tgt := contextFixture(t)
	writeFiles(t, root, "go.mod")
	ad := &GoAdapter{}

	sc, err := BuildScaffold(g, ad, root, tgt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TestPath != "internal/store/store_test.go" {
		t.Errorf("TestPath = %q", sc.TestPath)
	}
	if want := "go test -run ^TestStoreSave_Gen ./internal/store"; strings.Join(sc.RunCommand, " ") != want {
		t.Errorf("RunCommand = %v, want %q", sc.RunCommand, want)
	}
	if !strings.Contains(sc.PromptFragment, "TestStoreSave") {
		t.Errorf("PromptFragment missing prefix:\n%s", sc.PromptFragment)
	}
	if sc.Context == nil || !strings.Contains(sc.Context.Body, "Save") {
		t.Errorf("Context not assembled: %+v", sc.Context)
	}
}

// fakeProvider returns canned responses in order; the last repeats.
type fakeProvider struct {
	responses []string
	calls     int
}

func (f *fakeProvider) Send(_ context.Context, _ string, _ []provider.Message, _ []provider.ToolDef) (provider.Response, error) {
	i := f.calls
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	}
	f.calls++
	return provider.Response{
		Blocks:     []provider.Block{{Type: "text", Text: f.responses[i]}},
		StopReason: "end_turn",
	}, nil
}
func (f *fakeProvider) Name() string             { return "fake" }
func (f *fakeProvider) Model() string            { return "fake-model" }
func (f *fakeProvider) NativeContextWindow() int { return 8192 }

const cannedTest = "```go\npackage store\n\nimport \"testing\"\n\nfunc TestStoreSave(t *testing.T) {}\n```"

// fakeRunner scripts run outcomes: each call pops the next (output, err).
type runResult struct {
	out string
	err error
}

func scriptedRunner(t *testing.T, results []runResult) (Runner, *int) {
	t.Helper()
	calls := 0
	return func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		if calls >= len(results) {
			t.Fatal("runner called more times than scripted")
		}
		r := results[calls]
		calls++
		return r.out, r.err
	}, &calls
}

func pipelineFixture(t *testing.T, prov provider.Provider, run Runner) (Pipeline, string, Target) {
	t.Helper()
	g, root, tgt := contextFixture(t)
	writeFiles(t, root, "go.mod")
	return Pipeline{Graph: g, Adapter: &GoAdapter{}, Provider: prov, Run: run}, root, tgt
}

func TestGeneratePassFirstTry(t *testing.T) {
	run, calls := scriptedRunner(t, []runResult{{out: "ok  \tstore\t0.01s", err: nil}})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts != 1 || *calls != 1 {
		t.Fatalf("res = %+v, runner calls = %d", res, *calls)
	}
	data, err := os.ReadFile(filepath.Join(root, res.TestPath))
	if err != nil || !strings.Contains(string(data), "TestStoreSave") {
		t.Fatalf("written file: %v / %s", err, data)
	}
}

func TestGenerateRepairThenPass(t *testing.T) {
	run, _ := scriptedRunner(t, []runResult{
		{out: "--- FAIL: TestStoreSave\nFAIL", err: errors.New("exit status 1")},
		{out: "ok", err: nil},
	})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest, cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts != 2 {
		t.Fatalf("res = %+v", res)
	}
}

func TestGenerateExhaustedFIXME(t *testing.T) {
	fail := runResult{out: "--- FAIL: TestStoreSave\nFAIL", err: errors.New("exit status 1")}
	run, _ := scriptedRunner(t, []runResult{fail, fail, fail})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("err = %v, want ErrVerificationFailed", err)
	}
	if !res.FIXME || res.Attempts != 3 {
		t.Fatalf("res = %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(root, res.TestPath))
	first := strings.SplitN(string(data), "\n", 2)[0]
	if !strings.HasPrefix(first, "// FIXME: generated test failed verification") {
		t.Fatalf("first line = %q", first)
	}
}

func TestGenerateDiscardFailing(t *testing.T) {
	fail := runResult{out: "FAIL", err: errors.New("exit status 1")}
	run, _ := scriptedRunner(t, []runResult{fail, fail, fail})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2, DiscardFailing: true})
	if !errors.Is(err, ErrVerificationFailed) || !res.Discarded {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	if _, statErr := os.Stat(filepath.Join(root, res.TestPath)); !os.IsNotExist(statErr) {
		t.Fatal("discarded file still on disk")
	}
}

func TestGenerateDryRun(t *testing.T) {
	run, calls := scriptedRunner(t, nil) // must never run
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Code, "TestStoreSave") || *calls != 0 {
		t.Fatalf("res = %+v, runner calls = %d", res, *calls)
	}
	if _, statErr := os.Stat(filepath.Join(root, res.TestPath)); !os.IsNotExist(statErr) {
		t.Fatal("dry-run wrote a file")
	}
	if len(res.RunCommand) == 0 {
		t.Fatal("dry-run must report the run command")
	}
}

func TestGenerateNoCodeBlockThenRecover(t *testing.T) {
	run, _ := scriptedRunner(t, []runResult{{out: "ok", err: nil}})
	p, root, tgt := pipelineFixture(t,
		&fakeProvider{responses: []string{"I am unable to comply.", cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts != 2 {
		t.Fatalf("res = %+v", res)
	}
}

func TestGenerateNoTestsToRunIsFailure(t *testing.T) {
	run, _ := scriptedRunner(t, []runResult{
		{out: "testing: warning: no tests to run\nok", err: nil},
		{out: "ok", err: nil},
	})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest, cannedTest}}, run)

	res, err := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 {
		t.Fatalf("false pass: res = %+v", res)
	}
}

// TestGenerateJavaMockedRunner is the spec's Java integration: same
// pipeline, JavaAdapter, injected runner — no Maven/JDK in the suite.
func TestGenerateJavaMockedRunner(t *testing.T) {
	root := t.TempDir()
	src := "package com.acme;\n\npublic class Foo {\n\tpublic int bar(int x) {\n\t\treturn x + 1;\n\t}\n}\n"
	abs := filepath.Join(root, "src/main/java/com/acme/Foo.java")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, "pom.xml")
	nodes := []graph.Node{
		{ID: "src/main/java/com/acme/Foo.java::Foo.bar", Kind: graph.KindFunction, Name: "Foo.bar",
			Path: "src/main/java/com/acme/Foo.java", Line: 4, EndLine: 6, Language: "java",
			Params: "(int x)", ReturnType: "int", Exported: true},
	}
	g := query.NewGraph(nodes, nil)
	cannedJava := "```java\npackage com.acme;\n\nimport org.junit.jupiter.api.Test;\n\nclass FooTest {\n\t@Test\n\tvoid barAddsOne() {}\n}\n```"
	run, _ := scriptedRunner(t, []runResult{{out: "BUILD SUCCESS", err: nil}})
	p := Pipeline{Graph: g, Adapter: &JavaAdapter{}, Provider: &fakeProvider{responses: []string{cannedJava}}, Run: run}

	res, err := p.Generate(context.Background(), TargetFromNode(nodes[0]), Options{RepoRoot: root, MaxRepair: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.TestPath != "src/test/java/com/acme/FooTest.java" {
		t.Fatalf("res = %+v", res)
	}
	if want := "mvn -q test -Dtest=FooTest#barGen* -DfailIfNoTests=false"; strings.Join(res.RunCommand, " ") != want {
		t.Fatalf("RunCommand = %v, want %q", res.RunCommand, want)
	}
}

// TestGenerateSourceSafety is the spec's guard: the only file the pipeline
// may create or modify in the repo is the generated test.
func TestGenerateSourceSafety(t *testing.T) {
	fail := runResult{out: "FAIL", err: errors.New("exit status 1")}
	run, _ := scriptedRunner(t, []runResult{fail, fail, fail})
	p, root, tgt := pipelineFixture(t, &fakeProvider{responses: []string{cannedTest}}, run)

	before := snapshotTree(t, root)
	res, _ := p.Generate(context.Background(), tgt, Options{RepoRoot: root, MaxRepair: 2})
	after := snapshotTree(t, root)

	for path, sum := range after {
		if path == res.TestPath {
			continue
		}
		if prev, ok := before[path]; !ok || prev != sum {
			t.Errorf("pipeline touched %s", path)
		}
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRunDirResolution(t *testing.T) {
	// TS adapter implements runDir -> the target's package dir.
	ts := &TSAdapter{}
	if got := resolveRunDir(ts, "testdata/ws", Target{Name: "widget", Path: "packages/app/src/widget.ts", Language: "typescript"}); got != "packages/app" {
		t.Errorf("TS resolveRunDir = %q, want packages/app", got)
	}
	// Non-TS adapters do not implement runDir -> ".".
	if got := resolveRunDir(&GoAdapter{}, "testdata/ws", Target{Path: "internal/x/slug.go", Language: "go"}); got != "." {
		t.Errorf("Go resolveRunDir = %q, want \".\"", got)
	}
}

func TestCommentPrefix(t *testing.T) {
	if got := commentPrefix("python"); got != "#" {
		t.Fatalf("python prefix = %q, want #", got)
	}
	for _, lang := range []string{"go", "java", "typescript", ""} {
		if got := commentPrefix(lang); got != "//" {
			t.Fatalf("%s prefix = %q, want //", lang, got)
		}
	}
}
