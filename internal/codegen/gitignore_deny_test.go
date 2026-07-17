package codegen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIgnores inits a temp git repo, writes `gitignoreContent` to the named
// dotfile (".gitignore" or ".git/info/exclude"), and returns whether git
// considers `relPath` ignored. Uses `git check-ignore -q`: exit 0 => ignored,
// exit 1 => NOT ignored. Any other exit is a test fatal.
func gitIgnores(t *testing.T, dotfile, gitignoreContent, relPath string) bool {
	t.Helper()
	dir := t.TempDir()

	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	target := filepath.Join(dir, dotfile)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", dotfile, err)
	}
	if err := os.WriteFile(target, []byte(gitignoreContent), 0o644); err != nil {
		t.Fatalf("write %s: %v", dotfile, err)
	}

	cmd := exec.Command("git", "check-ignore", "-q", relPath)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true // exit 0 => ignored
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false // exit 1 => not ignored
		}
		t.Fatalf("git check-ignore %q: unexpected exit code %d: %s", relPath, exitErr.ExitCode(), exitErr.Stderr)
	}
	t.Fatalf("git check-ignore %q: %v", relPath, err)
	return false
}

// @s1 — the managed block is default-deny with a share re-include, and no
// longer enumerates individual artifact files under .tu-agent.
func TestGitignoreBlock_DefaultDenyShareReinclude(t *testing.T) {
	b := GitignoreBlock()

	if !strings.Contains(b, ".tu-agent/*") {
		t.Error("GitignoreBlock must ignore .tu-agent/* (default-deny)")
	}
	if !strings.Contains(b, "!.tu-agent/share/") {
		t.Error("GitignoreBlock must re-include the .tu-agent/share/ subtree")
	}

	for _, artifact := range []string{
		".tu-agent/graph.db",
		".tu-agent/memory.db",
		".tu-agent/telemetry.jsonl",
		".tu-agent/graph.build.lock",
	} {
		if strings.Contains(b, artifact) {
			t.Errorf("GitignoreBlock must no longer enumerate artifact %q", artifact)
		}
	}

	// A wholesale .tu-agent/ line would block the re-include.
	if strings.Contains(b, "\n.tu-agent/\n") {
		t.Error("a wholesale .tu-agent/ exclude blocks re-including the share subtree")
	}
}

// @s2 — a never-before-seen artifact is ignored without touching the block.
func TestGitignoreBlock_UnknownArtifactIgnored(t *testing.T) {
	if !gitIgnores(t, ".gitignore", MergeGitignore(""), ".tu-agent/somefeature/scratch.cache") {
		t.Error("invented .tu-agent/somefeature/scratch.cache should be ignored by default-deny")
	}
}

// @s3 — shared memory chunks remain committable, real artifacts stay ignored.
func TestGitignoreBlock_SharedChunksCommittable(t *testing.T) {
	if gitIgnores(t, ".gitignore", MergeGitignore(""), ".tu-agent/share/memory/chunks/chunk-alice.jsonl.gz") {
		t.Error("shared chunk .tu-agent/share/memory/chunks/chunk-alice.jsonl.gz must NOT be ignored")
	}
	if !gitIgnores(t, ".gitignore", MergeGitignore(""), ".tu-agent/memory/memory.db") {
		t.Error("artifact .tu-agent/memory/memory.db must still be ignored by default-deny")
	}
}

// @s4 — private mode (.git/info/exclude) re-includes the same share subtree.
func TestGitInfoExcludeBlock_SharesSubtree(t *testing.T) {
	if gitIgnores(t, ".git/info/exclude", MergeGitInfoExclude(""), ".tu-agent/share/memory/chunks/chunk-alice.jsonl.gz") {
		t.Error("private mode must NOT ignore .tu-agent/share/memory/chunks/chunk-alice.jsonl.gz")
	}
	if !gitIgnores(t, ".git/info/exclude", MergeGitInfoExclude(""), ".tu-agent/memory/memory.db") {
		t.Error("private mode must still ignore .tu-agent/memory/memory.db")
	}
	if !gitIgnores(t, ".git/info/exclude", MergeGitInfoExclude(""), ".tu-agent/graph/graph.db") {
		t.Error("private mode must still ignore .tu-agent/graph/graph.db")
	}

	b := GitInfoExcludeBlock()
	if strings.Contains(b, "!.tu-agent/memory/chunks/") {
		t.Error("GitInfoExcludeBlock must no longer re-include the old .tu-agent/memory/chunks/")
	}
	if !strings.Contains(b, "!.tu-agent/share/") {
		t.Error("GitInfoExcludeBlock must re-include the new .tu-agent/share/ subtree")
	}
}
