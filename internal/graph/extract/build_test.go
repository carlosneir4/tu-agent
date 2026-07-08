package extract

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/store"
)

// writeFixture writes src to dir/relPath, creating parent dirs.
func writeFixture(t *testing.T, dir, relPath, src string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

const serviceJava = `
package com.acme;
public class Service {
    public void run() {}
}
`

const serviceTestJava = `
package com.acme;
import org.junit.Test;
public class ServiceTest {
    @Test public void testRun() {}
}
`

func TestBuildFull(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/Service.java", serviceJava)
	writeFixture(t, dir, "src/ServiceTest.java", serviceTestJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("Build: %v", err)
	}

	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	var classCount int
	for _, n := range nodes {
		if n.Kind == graph.KindClass || n.Kind == graph.KindTest {
			classCount++
		}
	}
	if classCount < 2 {
		t.Errorf("expected >=2 class/test nodes, got %d; nodes: %+v", classCount, nodes)
	}

	edges, err := st.AllEdges()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	var foundTestedBy bool
	for _, e := range edges {
		if e.Kind == graph.EdgeTestedBy {
			foundTestedBy = true
		}
	}
	if !foundTestedBy {
		t.Errorf("expected tested_by edge; edges: %+v", edges)
	}
}

func TestBuildReparsesWhenNodesWiped(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/Service.java", serviceJava)
	writeFixture(t, dir, "src/ServiceTest.java", serviceTestJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	before, err := st.NodeCount()
	if err != nil {
		t.Fatalf("NodeCount: %v", err)
	}
	if before == 0 {
		t.Fatal("expected nodes after first build")
	}

	// Simulate an external node-store wipe that leaves the file-state table
	// intact (a mid-session graph.db reset / lost -wal sidecar): drop every
	// file's nodes but keep its files row, so the reconcile would see all files
	// stat/SHA "unchanged".
	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	for path := range files {
		if err := st.ReplaceFileNodes(path, nil, nil, nil); err != nil {
			t.Fatalf("wipe nodes for %s: %v", path, err)
		}
	}
	if n, _ := st.NodeCount(); n != 0 {
		t.Fatalf("expected 0 nodes after wipe, got %d", n)
	}

	// Re-build: the files table matches disk, so without the empty-node guard
	// every file is skipped and the graph stays permanently empty.
	res, err := Build(dir, []string{".java"}, st)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if res.Parsed == 0 {
		t.Errorf("expected files re-parsed after node wipe, got Parsed=0 (unchanged=%d)", res.Unchanged)
	}
	after, err := st.NodeCount()
	if err != nil {
		t.Fatalf("NodeCount after rebuild: %v", err)
	}
	if after == 0 {
		t.Error("graph still empty after rebuild: empty-node reconcile guard did not fire")
	}
}

func TestBuildIncremental(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/Service.java", serviceJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// First build.
	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Second build — no file changes. Parse should not run again.
	// We verify by checking the SHA stored vs. the actual file hash.
	src, _ := os.ReadFile(filepath.Join(dir, "src/Service.java"))
	expected := fileSHA256(src)
	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	rec, ok := files["src/Service.java"]
	if !ok {
		t.Fatal("src/Service.java not in files table after first build")
	}
	if rec.SHA256 != expected {
		t.Errorf("stored sha256 %q != computed %q", rec.SHA256, expected)
	}

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	// Still one file row.
	files, err = st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file row after incremental build, got %d", len(files))
	}
}

func TestBuildRecordsFileSize(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/com/acme/core/BaseService.java", serviceJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("Build: %v", err)
	}
	filesAfter, _ := st.Files()
	if filesAfter["src/com/acme/core/BaseService.java"].Size == 0 {
		t.Errorf("file size not recorded after build")
	}
}

func TestBuildDeletesRemovedFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/A.java", serviceJava)
	writeFixture(t, dir, "src/B.java", serviceTestJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Remove B.java then rebuild.
	if err := os.Remove(filepath.Join(dir, "src/B.java")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("second Build: %v", err)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if _, exists := files["src/B.java"]; exists {
		t.Error("B.java should have been deleted from the store")
	}
	if _, exists := files["src/A.java"]; !exists {
		t.Error("A.java should still be in the store")
	}
}

func TestBuildSkipsDotDirs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/Service.java", serviceJava)
	// A worktree copy under a dot-dir must not be scanned: it duplicates files
	// and inflates the graph.
	writeFixture(t, dir, ".claire/worktrees/feature/src/Service.java", serviceJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("Build: %v", err)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if _, ok := files["src/Service.java"]; !ok {
		t.Error("src/Service.java should be in the store")
	}
	if _, ok := files[".claire/worktrees/feature/src/Service.java"]; ok {
		t.Error("files under dot-dirs must be skipped")
	}
}

func TestBuildSkipsTestdataAndFixtures(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	writeFixture(t, dir, "src/Service.java", serviceJava)
	// testdata/ (a Go convention) and fixtures/ trees hold sample code used by
	// tests, not production code. They must not enter the graph, or they get
	// promoted to spurious concept cards by the learn pipeline.
	writeFixture(t, dir, "internal/testgen/testdata/Sample.java", serviceJava)
	writeFixture(t, dir, "scripts/fixtures/java-sample/Sample.java", serviceJava)

	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Build(dir, []string{".java"}, st); err != nil {
		t.Fatalf("Build: %v", err)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if _, ok := files["src/Service.java"]; !ok {
		t.Error("src/Service.java should be in the store")
	}
	if _, ok := files["internal/testgen/testdata/Sample.java"]; ok {
		t.Error("files under testdata/ must be skipped")
	}
	if _, ok := files["scripts/fixtures/java-sample/Sample.java"]; ok {
		t.Error("files under fixtures/ must be skipped")
	}
}

func TestBuild_multiLanguage(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Svc.java", "package a;\npublic class Svc {}\n")
	write("go/main.go", "package main\nfunc main() {}\n")
	write("py/app.py", "class App:\n    pass\n")

	st, err := store.Open(filepath.Join(t.TempDir(), "g.db"), ExtractorVersion)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	res, err := Build(root, Extensions(), st)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Parsed != 3 || res.Failed != 0 {
		t.Fatalf("result = %+v, want Parsed=3 Failed=0", res)
	}

	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatal(err)
	}
	langs := map[string]bool{}
	for _, n := range nodes {
		langs[n.Language] = true
	}
	for _, want := range []string{"java", "go", "python"} {
		if !langs[want] {
			t.Errorf("no %s nodes in graph; languages present: %v", want, langs)
		}
	}
}

const signatureSvcGo = `package billing

type Invoice struct{}

func Process(invoice Invoice) (int, error) { return 0, nil }
`

func TestBuild_exportedFlagPersisted(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package pkg\n\nfunc Pub() int { return 1 }\nfunc priv() int { return 2 }\n"
	if err := os.WriteFile(filepath.Join(root, "pkg", "svc.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	if _, err := Build(root, Extensions(), st); err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	exported := map[string]bool{}
	for _, n := range nodes {
		exported[n.ID] = n.Exported
	}
	if !exported["pkg/svc.go::Pub"] {
		t.Errorf("pkg/svc.go::Pub not exported after Build+AllNodes roundtrip")
	}
	if exported["pkg/svc.go::priv"] {
		t.Errorf("pkg/svc.go::priv exported after Build+AllNodes roundtrip")
	}
}

func TestBuildPersistsSignatures(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "billing/svc.go", signatureSvcGo)

	st, err := store.Open(filepath.Join(dir, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if _, err := Build(dir, []string{".go"}, st); err != nil {
		t.Fatalf("Build: %v", err)
	}

	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	for _, n := range nodes {
		if n.ID == "billing/svc.go::Process" {
			if n.Params != "(invoice Invoice)" || n.ReturnType != "(int, error)" {
				t.Errorf("signature = %q / %q", n.Params, n.ReturnType)
			}
			return
		}
	}
	t.Fatalf("Process node not found in %d nodes", len(nodes))
}

func TestSkipBuildPath(t *testing.T) {
	cases := map[string]bool{
		"src/a.ts":                 false,
		"src/components/Btn.tsx":   false,
		"src/__generated__/q.ts":   true,
		"packages/x/testdata/y.ts": true,
		"fixtures/z.ts":            true,
		".storybook/main.ts":       true,
		"src/.eslintrc.ts":         true,
	}
	for rel, want := range cases {
		if got := skipBuildPath(rel); got != want {
			t.Errorf("skipBuildPath(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestSkipBuildDirGenerated(t *testing.T) {
	if !skipBuildDir("__generated__") {
		t.Error("skipBuildDir must skip __generated__")
	}
}

// gitFixtureWrite creates dir/rel (with parents) containing body.
func gitFixtureWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnumerateGitFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")

	gitFixtureWrite(t, dir, ".gitignore", "node_modules/\ndist/\n")
	gitFixtureWrite(t, dir, "src/a.ts", "export const a = 1")
	gitFixtureWrite(t, dir, "node_modules/pkg/b.ts", "export const b = 2") // ignored
	gitFixtureWrite(t, dir, "dist/c.ts", "export const c = 3")             // ignored
	gitFixtureWrite(t, dir, "src/__generated__/g.ts", "export const g = 4")
	gitFixtureWrite(t, dir, "testdata/d.ts", "export const d = 5")
	git("add", "-A") // respects .gitignore; node_modules/dist not staged
	// Untracked-but-not-ignored source created after add:
	gitFixtureWrite(t, dir, "src/u.tsx", "export const u = 6")

	got, ok := enumerateGitFiles(dir, map[string]struct{}{".ts": {}, ".tsx": {}})
	if !ok {
		t.Fatal("expected ok=true inside a git repo")
	}
	want := map[string]struct{}{"src/a.ts": {}, "src/u.tsx": {}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in %v", k, got)
		}
	}
}

func TestEnumerateGitFiles_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if _, ok := enumerateGitFiles(dir, map[string]struct{}{".ts": {}}); ok {
		t.Fatal("expected ok=false outside a git repo")
	}
}

// TestBuildGit_DeletedUnstaged verifies that Build does not abort when a
// tracked file has been deleted from the working tree without staging the
// deletion ("rm foo" without "git rm"). Before the fix, enumerateGitFiles
// returned the dead path via --cached, Build tried os.ReadFile on it, got
// ENOENT, and returned an error — breaking the graph-update hook mid-refactor.
func TestBuildGit_DeletedUnstaged(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")

	// .gitignore so node_modules/ is excluded from git tracking.
	gitFixtureWrite(t, dir, ".gitignore", "node_modules/\n")

	// Tracked source file that should produce nodes after Build.
	gitFixtureWrite(t, dir, "src/a.ts", "export const greet = (name: string): string => `Hello ${name}`;\n")

	// Ignored file that must never enter the store.
	gitFixtureWrite(t, dir, "node_modules/dep.ts", "export const dep = 1;\n")

	// Second tracked file — will be deleted from disk WITHOUT staging the deletion.
	gitFixtureWrite(t, dir, "src/gone.ts", "export const gone = true;\n")

	git("add", "-A")

	// Delete src/gone.ts from disk without running "git rm" — the defect scenario.
	if err := os.Remove(filepath.Join(dir, "src", "gone.ts")); err != nil {
		t.Fatalf("remove gone.ts: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	st, err := store.Open(dbPath, ExtractorVersion)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	// Build must not return an error — deleted-unstaged files must be silently
	// skipped, not treated as a fatal read failure.
	if _, err := Build(dir, []string{".ts"}, st); err != nil {
		t.Fatalf("Build returned error for deleted-unstaged file: %v", err)
	}

	files, err := st.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	// src/a.ts must be present (it exists on disk and is tracked).
	if _, ok := files["src/a.ts"]; !ok {
		t.Error("src/a.ts should be in the store after Build")
	}

	// src/gone.ts must not be in the store — it was deleted from disk.
	if _, ok := files["src/gone.ts"]; ok {
		t.Error("src/gone.ts must not be in the store (deleted-unstaged)")
	}

	// node_modules/dep.ts must not be in the store — it is git-ignored.
	if _, ok := files["node_modules/dep.ts"]; ok {
		t.Error("node_modules/dep.ts must not be in the store (git-ignored)")
	}

	// src/a.ts must have produced at least one node.
	nodes, err := st.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	var foundA bool
	for _, n := range nodes {
		if strings.HasPrefix(n.ID, "src/a.ts") {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Errorf("expected nodes from src/a.ts, got none; all nodes: %+v", nodes)
	}
}

func TestEnumerateFiles_FallbackNonGit(t *testing.T) {
	dir := t.TempDir() // no git init -> fallback to walk
	gitFixtureWrite(t, dir, "src/a.ts", "export const a = 1")
	gitFixtureWrite(t, dir, "src/__generated__/g.ts", "export const g = 2")
	gitFixtureWrite(t, dir, "testdata/d.ts", "export const d = 3")

	got, err := enumerateFiles(dir, map[string]struct{}{".ts": {}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["src/a.ts"]; !ok {
		t.Errorf("fallback walk should find src/a.ts: %v", got)
	}
	if _, ok := got["src/__generated__/g.ts"]; ok {
		t.Errorf("fallback walk must skip __generated__: %v", got)
	}
	if _, ok := got["testdata/d.ts"]; ok {
		t.Errorf("fallback walk must skip testdata: %v", got)
	}
}
