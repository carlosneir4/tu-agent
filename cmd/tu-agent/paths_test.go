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
	want := filepath.Join("/repo", ".tu-agent", "memory", "memory.db")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPathHelpersPerSubsystemLayout pins the NEW per-subsystem .tu-agent tree
// (dir-relayout, scenarios @s1 @s2 @s3 @s4). Each helper must nest its artifact
// under a subsystem directory instead of the old flat root. These are RED
// against the current flat helpers and turn GREEN when the flip lands.
func TestPathHelpersPerSubsystemLayout(t *testing.T) {
	const root = "/repo"
	cases := []struct {
		name string // @s tag
		got  string // helper output
		want string // expected new path
	}{
		{
			name: "s1-memoryDB",
			got:  memoryDBPath(root),
			want: filepath.Join(root, ".tu-agent", "memory", "memory.db"),
		},
		{
			name: "s2-graphDB",
			got:  graphDBPath(root),
			want: filepath.Join(root, ".tu-agent", "graph", "graph.db"),
		},
		{
			name: "s3-telemetry",
			got:  telemetryPath(root),
			want: filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"),
		},
		{
			name: "s4-memoryChunks",
			got:  memoryChunksDir(root),
			want: filepath.Join(root, ".tu-agent", "share", "memory", "chunks"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
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

// cwdRelativeViolations returns the CWD-relative needles present in src.
// flatNeedles are checked in every file; liveNeedles only in "live" files —
// those NOT carrying a cobra `Deprecated:` field (the frozen standalone
// commands intentionally keep passing "." and have no user).
func cwdRelativeViolations(src string, flatNeedles, liveNeedles []string) []string {
	found := make([]string, 0, len(flatNeedles)+len(liveNeedles))
	for _, n := range flatNeedles {
		if strings.Contains(src, n) {
			found = append(found, n)
		}
	}
	if !strings.Contains(src, "Deprecated:") {
		for _, n := range liveNeedles {
			if strings.Contains(src, n) {
				found = append(found, n)
			}
		}
	}
	return found
}

// TestNoCWDRelativeStorePaths fails if any non-test source file reintroduces a
// CWD-relative store or generated-artifact path. These locations must resolve
// through repoRoot(); a literal "." anchors them to wherever the process happens
// to run, which created stray per-subdirectory .tu-agent / .claude / CLAUDE.md /
// .mcp.json artifacts when learn/test ran below the repo root. Scan/analysis
// roots (where to walk source) may stay relative — only writes are pinned here.
//
// The needles split in two:
//   - flatNeedles are checked in EVERY non-test file.
//   - liveNeedles are checked only in "live" files — those NOT carrying a cobra
//     `Deprecated:` field. telemetryPath(".") is live-only: the frozen standalone
//     commands (chat.go, run.go) keep passing "." and have no user, so pinning
//     the needle flat would fire on them; a NEW live command that reintroduces
//     the same read-side bug (the one that made stats read the wrong file from a
//     subdir) IS caught.
func TestNoCWDRelativeStorePaths(t *testing.T) {
	dot := "(\".\")"
	flatNeedles := []string{
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
	// Live-only: the frozen chat/run keep passing "." (they warn on use), so
	// this needle is exempt in files carrying `Deprecated:` and enforced
	// everywhere else — stats.go and any future live command.
	liveNeedles := []string{"telemetryPath" + dot}

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
		for _, n := range cwdRelativeViolations(string(b), flatNeedles, liveNeedles) {
			t.Errorf("%s contains CWD-relative store path %q — use repoRoot()", f, n)
		}
	}
}

// TestCWDGuardScopesLiveOnlyNeedles (@s2) pins the live-only scoping rule of
// cwdRelativeViolations directly, with synthetic inputs. The rule has its own
// silent-failure mode: a predicate too broad stops guarding a live file and says
// nothing. A live source (no `Deprecated:`) with telemetryPath(".") is a
// violation; a frozen source carrying `Deprecated:` with the same call is exempt.
func TestCWDGuardScopesLiveOnlyNeedles(t *testing.T) {
	liveNeedles := []string{`telemetryPath(".")`}

	liveSrc := `func run() { entries, _ := stats.ReadEntries(telemetryPath(".")) }`
	if got := cwdRelativeViolations(liveSrc, nil, liveNeedles); len(got) == 0 {
		t.Errorf("live source with telemetryPath(\".\") must be a violation, got none")
	}

	frozenSrc := `var runCmd = &cobra.Command{Deprecated: "use the plugin", RunE: func() error {
		entries, _ := stats.ReadEntries(telemetryPath("."))
		return nil
	}}`
	if got := cwdRelativeViolations(frozenSrc, nil, liveNeedles); len(got) != 0 {
		t.Errorf("frozen source carrying Deprecated: must be exempt, got %v", got)
	}
}
