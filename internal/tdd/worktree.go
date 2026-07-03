package tdd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Snapshot captures the current working tree as a tree-ish without
// committing. It builds the tree in an isolated, private temporary index
// file (via GIT_INDEX_FILE) seeded from HEAD and updated with `git add -A`,
// so untracked files and deletions are included. It never touches the
// repo's real index, working tree, or HEAD.
func Snapshot(ctx context.Context, root string) (string, error) {
	idx, err := os.CreateTemp("", "tu-tdd-index-*")
	if err != nil {
		return "", fmt.Errorf("tdd.Snapshot: %w", err)
	}
	idxPath := idx.Name()
	if cerr := idx.Close(); cerr != nil {
		_ = os.Remove(idxPath)
		return "", fmt.Errorf("tdd.Snapshot: %w", cerr)
	}
	defer func() { _ = os.Remove(idxPath) }()

	env := []string{"GIT_INDEX_FILE=" + idxPath}

	// Seed the temp index from HEAD's tree.
	if _, err := gitWithEnv(ctx, root, env, "read-tree", "HEAD"); err != nil {
		return "", fmt.Errorf("tdd.Snapshot: %w", err)
	}

	// Update the temp index to match the working tree, including untracked
	// files and deletions, without touching the real index.
	if _, err := gitWithEnv(ctx, root, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("tdd.Snapshot: %w", err)
	}

	out, err := gitWithEnv(ctx, root, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("tdd.Snapshot: %w", err)
	}

	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("tdd.Snapshot: git write-tree returned empty sha")
	}
	return sha, nil
}

// DiffFiles returns the repo-relative paths changed between two tree-ishes.
func DiffFiles(ctx context.Context, root, from, to string) ([]string, error) {
	out, err := git(ctx, root, "diff", "--name-only", from, to)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// PartitionTests splits paths into test files and production files by
// convention: Java `src/test/` segments and Go `_test.go` suffixes are tests.
func PartitionTests(paths []string) (tests, prod []string) {
	for _, p := range paths {
		if isTestPath(p) {
			tests = append(tests, p)
		} else {
			prod = append(prod, p)
		}
	}
	return tests, prod
}

func isTestPath(p string) bool {
	q := strings.ReplaceAll(p, "\\", "/")
	switch {
	case strings.HasSuffix(q, "_test.go"):
		return true
	case strings.HasPrefix(q, "src/test/") || strings.Contains(q, "/src/test/"):
		return true
	default:
		return false
	}
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	return gitWithEnv(ctx, root, nil, args...)
}

// gitWithEnv runs git with extra environment variables appended to the
// process environment (e.g. GIT_INDEX_FILE to target a private, isolated
// index file instead of the repo's real index).
func gitWithEnv(ctx context.Context, root string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tdd.git %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
