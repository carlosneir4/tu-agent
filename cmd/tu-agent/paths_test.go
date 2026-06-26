package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedSkillsDir(t *testing.T) {
	got := generatedSkillsDir("/repo")
	want := filepath.Join("/repo", ".claude", "skills")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMemoryDBPath(t *testing.T) {
	got := memoryDBPath("/repo")
	want := filepath.Join("/repo", ".tu-agent", "memory.db")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRepoRootFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got, err := filepath.EvalSymlinks(repoRoot())
	if err != nil {
		t.Fatalf("EvalSymlinks(repoRoot()): %v", err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("repoRoot() = %q, want repo root %q", got, want)
	}
}

func TestRepoRootDetectsGitFile(t *testing.T) {
	// A worktree/submodule has .git as a FILE, not a directory.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got, err := filepath.EvalSymlinks(repoRoot())
	if err != nil {
		t.Fatalf("EvalSymlinks(repoRoot()): %v", err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("repoRoot() with .git file = %q, want %q", got, want)
	}
}

func TestRepoRootFallsBackToCwdOutsideRepo(t *testing.T) {
	// A temp dir with no .git in it or any ancestor must yield ".".
	dir := t.TempDir()
	chdir(t, dir)
	if got := repoRoot(); got != "." {
		t.Errorf("repoRoot() outside a repo = %q, want \".\"", got)
	}
}

// TestNoCWDRelativeStorePaths fails if any non-test source file reintroduces a
// CWD-relative store or generated-artifact path. These locations must resolve
// through repoRoot(); a literal "." anchors them to wherever the process happens
// to run, which created stray per-subdirectory .tu-agent / .claude / CLAUDE.md /
// .mcp.json artifacts when learn/test ran below the repo root. Scan/analysis
// roots (where to walk source) may stay relative — only writes are pinned here.
func TestNoCWDRelativeStorePaths(t *testing.T) {
	dot := "(\".\")"
	needles := []string{
		"memoryDBPath" + dot,
		"memoryChunksDir" + dot,
		"graphDBPath" + dot,
		"MkdirAll(\".tu-agent\"",
		// telemetry.jsonl written relative to CWD instead of repoRoot
		"filepath.Join(\".tu-agent\"",
		// generated .claude/skills, CLAUDE.md, knowledge graph at CWD
		"generatedSkillsDir" + dot,
		"registerKnowledge" + dot,
		"writeKnowledgeBlock(\"CLAUDE.md\")",
		// synthesize/enrich (fingerprints + telemetry) anchored at CWD
		"mergedSynthesizeAndEnrich(ctx, \".\"",
		// .mcp.json written to the scan root instead of repoRoot
		"writeMCPConfig(root)",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, n := range needles {
			if strings.Contains(src, n) {
				t.Errorf("%s contains CWD-relative store path %q — use repoRoot()", f, n)
			}
		}
	}
}
