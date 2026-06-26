package extract

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/store"
)

// BuildResult summarises one Build or Update run.
type BuildResult struct {
	Parsed    int
	Unchanged int
	Deleted   int
	Failed    int
}

// Build does a full parse + resolve pass over root. Only files whose SHA-256
// has changed since the last build are re-parsed. Files no longer on disk are
// deleted from the store. After parsing, a project-wide resolve runs and
// persists all non-contains edges.
func Build(root string, exts []string, st *store.Store) (BuildResult, error) {
	var result BuildResult
	extSet := map[string]struct{}{}
	for _, e := range exts {
		extSet[e] = struct{}{}
	}

	// 1. Collect files to parse (git non-ignored, or filesystem walk fallback).
	onDisk, err := enumerateFiles(root, extSet)
	if err != nil {
		return result, err
	}

	// 2. Load known files from the store.
	known, err := st.Files()
	if err != nil {
		return result, fmt.Errorf("graph.Build: loading files: %w", err)
	}

	// 3. Delete files removed from disk.
	for path := range known {
		if _, exists := onDisk[path]; !exists {
			if err := st.DeleteFile(path); err != nil {
				return result, fmt.Errorf("graph.Build: deleting %s: %w", path, err)
			}
			result.Deleted++
		}
	}

	// 4. Parse changed or new files.
	for relPath := range onDisk {
		full := filepath.Join(root, filepath.FromSlash(relPath))
		src, err := os.ReadFile(full)
		if err != nil {
			return result, fmt.Errorf("graph.Build: reading %s: %w", relPath, err)
		}
		sum := fileSHA256(src)
		if rec, ok := known[relPath]; ok && rec.SHA256 == sum {
			result.Unchanged++
			continue // unchanged
		}
		facts, err := dispatchParse(relPath, src)
		if err != nil {
			slog.Warn("graph.Build: parse failed", "path", relPath, "err", err)
			_ = st.UpsertFile(store.FileRecord{
				Path: relPath, SHA256: sum, Language: strings.ToLower(strings.TrimPrefix(filepath.Ext(relPath), ".")),
				Status: "failed",
			})
			result.Failed++
			continue
		}
		if err := st.UpsertFile(store.FileRecord{
			Path: relPath, SHA256: sum, Language: facts.Meta.Language,
			Status: facts.Meta.Status, Package: facts.Meta.Package,
			Imports: facts.Meta.Imports, Size: len(src),
		}); err != nil {
			return result, fmt.Errorf("graph.Build: upsert file %s: %w", relPath, err)
		}
		if err := st.ReplaceFileNodes(relPath, facts.Nodes, facts.Refs, facts.Contains); err != nil {
			return result, fmt.Errorf("graph.Build: replace nodes %s: %w", relPath, err)
		}
		result.Parsed++
	}

	// 5. Resolve project-wide.
	return result, resolveFromStore(st, goModulePath(root))
}

// goModulePath reads the module path from root/go.mod. Returns "" when there is
// no go.mod (non-Go project) or it cannot be parsed; callers treat "" as "no
// in-module Go import resolution".
func goModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// resolveFromStore loads all nodes, metas, and refs from the store, runs
// Resolve, and persists the result.
func resolveFromStore(st *store.Store, modulePath string) error {
	nodes, err := st.AllNodes()
	if err != nil {
		return fmt.Errorf("graph.resolveFromStore: %w", err)
	}
	rawRefs, err := st.AllRefs()
	if err != nil {
		return fmt.Errorf("graph.resolveFromStore: %w", err)
	}
	fileRecs, err := st.Files()
	if err != nil {
		return fmt.Errorf("graph.resolveFromStore: %w", err)
	}
	metas := make([]graph.FileMeta, 0, len(fileRecs))
	for _, rec := range fileRecs {
		metas = append(metas, graph.FileMeta{
			Path: rec.Path, Language: rec.Language,
			Package: rec.Package, Imports: rec.Imports,
		})
	}
	edges, extNodes := ResolveWithNodes(nodes, metas, rawRefs, modulePath)
	if err := st.ReplaceResolvedEdges(edges, extNodes); err != nil {
		return fmt.Errorf("graph.resolveFromStore: %w", err)
	}
	return nil
}

// dispatchParse routes a file to the appropriate parser by extension.
func dispatchParse(relPath string, src []byte) (*FileFacts, error) {
	if p := parserFor(relPath); p != nil {
		return p(relPath, src)
	}
	return nil, fmt.Errorf("no parser for %s", strings.ToLower(filepath.Ext(relPath)))
}

func fileSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// walkFiles discovers parseable files by walking the filesystem under root.
// Fallback for non-git directories; preserves the original pre-git behavior.
func walkFiles(root string, extSet map[string]struct{}) (map[string]struct{}, error) {
	onDisk := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip dot-directories (.git, .tu-agent, .claire worktrees, …) and
			// test-fixture trees (testdata, fixtures): the former hold VCS
			// metadata or worktree copies that duplicate and inflate the graph;
			// the latter hold sample code that is not production and otherwise
			// gets promoted to spurious concept cards. The root itself is never
			// skipped, even if its name matches.
			if path != root && skipBuildDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := extSet[strings.ToLower(filepath.Ext(rel))]; ok {
			onDisk[rel] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("graph.Build: walking %s: %w", root, err)
	}
	return onDisk, nil
}

// enumerateFiles returns the root-relative paths to parse: git-tracked and
// untracked-non-ignored files when root is a git repo, else a filesystem walk.
func enumerateFiles(root string, extSet map[string]struct{}) (map[string]struct{}, error) {
	if files, ok := enumerateGitFiles(root, extSet); ok {
		return files, nil
	}
	return walkFiles(root, extSet)
}

// skipBuildDir reports whether a directory should be excluded from the graph
// walk: dot-directories (VCS metadata, worktree copies) and test-fixture trees
// (testdata — a Go convention the go tool itself ignores — and fixtures). The
// caller exempts the root directory.
func skipBuildDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "testdata", "fixtures", "__generated__":
		return true
	}
	return false
}

// skipBuildPath reports whether a slash-separated, root-relative path lies in a
// tree excluded from the graph: any dot-prefixed segment, or a testdata,
// fixtures, or __generated__ segment. Used for git-enumerated paths, which
// skipBuildDir (single dir-name based) cannot filter on its own.
func skipBuildPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ".") {
			return true
		}
		switch seg {
		case "testdata", "fixtures", "__generated__":
			return true
		}
	}
	return false
}

// enumerateGitFiles lists non-ignored files under root via git, returning the
// set of root-relative paths whose extension is in extSet and that pass
// skipBuildPath, with ok=true. ok=false means root is not a git repository or
// git is unavailable; the caller must fall back to a filesystem walk.
func enumerateGitFiles(root string, extSet map[string]struct{}) (map[string]struct{}, bool) {
	cmd := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	onDisk := map[string]struct{}{}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		if _, ok := extSet[strings.ToLower(filepath.Ext(rel))]; !ok {
			continue
		}
		if skipBuildPath(rel) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue
		}
		onDisk[rel] = struct{}{}
	}
	return onDisk, true
}
