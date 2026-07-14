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

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// BuildResult summarises one Build or Update run.
type BuildResult struct {
	Parsed    int
	Unchanged int
	Deleted   int
	Failed    int
}

// Build does a full parse + resolve pass over root. See BuildScoped.
func Build(root string, exts []string, st *store.Store) (BuildResult, error) {
	return BuildScoped(root, "", exts, st)
}

// underScope reports whether the slash-separated repo-relative path rel lies
// under scope (a slash-separated dir prefix). Empty scope matches everything.
func underScope(rel, scope string) bool {
	if scope == "" {
		return true
	}
	return rel == scope || strings.HasPrefix(rel, scope+"/")
}

// BuildScoped parses + resolves files under root. A non-empty scope restricts
// the pass to that repo-relative subtree: only in-scope files are enumerated,
// and only in-scope store entries whose file vanished are deleted — entries
// outside the scope are never touched. Only files whose SHA-256 changed are
// re-parsed. After parsing, a project-wide resolve runs.
func BuildScoped(root, scope string, exts []string, st *store.Store) (BuildResult, error) {
	var result BuildResult

	unlock, err := acquireBuildLock(root)
	if err != nil {
		return result, err
	}
	defer unlock()

	extSet := map[string]struct{}{}
	for _, e := range exts {
		extSet[e] = struct{}{}
	}

	// 1. Collect files to parse (git non-ignored, or filesystem walk fallback).
	onDisk, err := enumerateFiles(root, extSet)
	if err != nil {
		return result, err
	}
	if scope != "" {
		for path := range onDisk {
			if !underScope(path, scope) {
				delete(onDisk, path)
			}
		}
	}

	// 2. Load known files from the store.
	known, err := st.Files()
	if err != nil {
		return result, fmt.Errorf("graph.Build: loading files: %w", err)
	}

	// A node-store wipe that leaves the file-state table intact (an external
	// graph.db reset, a lost -wal sidecar) would otherwise make every file look
	// "unchanged" below, so nothing re-parses and the graph stays silently
	// empty. If the store claims successfully-parsed files yet holds zero nodes,
	// force a full re-parse to rebuild them.
	//
	// Only for a full (unscoped) build: a scoped build can repopulate just its
	// own subtree, so forcing it would partially refill the node store, push
	// NodeCount above zero, and thereby suppress both this guard and the
	// empty-graph warning for the other subtrees that stay node-less. A scoped
	// build over a wiped store is left to be healed by a full learn/graph update.
	forceReparse := false
	if scope == "" {
		if n, err := st.NodeCount(); err != nil {
			return result, fmt.Errorf("graph.Build: node count: %w", err)
		} else if n == 0 && anyParsed(known) {
			forceReparse = true
		}
	}

	// 3. Delete files removed from disk.
	for path := range known {
		if !underScope(path, scope) {
			continue
		}
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
		rec, knownFile := known[relPath]
		if fi, err := os.Stat(full); err == nil && knownFile && !forceReparse &&
			rec.MtimeNS == fi.ModTime().UnixNano() && rec.Size == int(fi.Size()) {
			result.Unchanged++
			continue // stat-identical: skip read + hash entirely
		}
		src, err := os.ReadFile(full)
		if err != nil {
			slog.Warn("graph.Build: reading file, skipping", "path", relPath, "err", err)
			continue
		}
		fi, err := os.Stat(full)
		if err != nil {
			slog.Warn("graph.Build: stat file, skipping", "path", relPath, "err", err)
			continue
		}
		sum := fileSHA256(src)
		if knownFile && !forceReparse && rec.SHA256 == sum {
			// touched but identical: refresh the stat so the next run fast-paths.
			rec.MtimeNS = fi.ModTime().UnixNano()
			rec.Size = int(fi.Size())
			if err := st.UpsertFile(rec); err != nil {
				return result, fmt.Errorf("graph.Build: refresh stat %s: %w", relPath, err)
			}
			result.Unchanged++
			continue
		}
		facts, err := dispatchParse(relPath, src)
		if err != nil {
			slog.Warn("graph.Build: parse failed", "path", relPath, "err", err)
			_ = st.UpsertFile(store.FileRecord{
				Path: relPath, SHA256: sum, Language: strings.ToLower(strings.TrimPrefix(filepath.Ext(relPath), ".")),
				Status: "failed", Size: len(src), MtimeNS: fi.ModTime().UnixNano(),
			})
			result.Failed++
			continue
		}
		// Persist the file-state row and its nodes in one transaction: the file
		// row is this loop's change-detection key, so it must never land ahead of
		// its nodes (see store.ReplaceFileAndNodes).
		if err := st.ReplaceFileAndNodes(store.FileRecord{
			Path: relPath, SHA256: sum, Language: facts.Meta.Language,
			Status: facts.Meta.Status, Package: facts.Meta.Package,
			Imports: facts.Meta.Imports, Size: len(src), MtimeNS: fi.ModTime().UnixNano(),
		}, facts.Nodes, facts.Refs, facts.Contains); err != nil {
			return result, fmt.Errorf("graph.Build: persist %s: %w", relPath, err)
		}
		result.Parsed++
	}

	// 5. Resolve project-wide.
	return result, resolveFromStore(st, goModulePath(root))
}

// acquireBuildLock takes an exclusive, blocking advisory lock on
// "<root>/.tu-agent/graph.build.lock" so concurrent BuildScoped calls against
// the same root single-flight instead of racing the store. It returns a
// release func the caller must defer; the lock file itself is created (but
// not locked) on every platform, while the actual flock is unix-only — see
// lock_unix.go / lock_windows.go.
func acquireBuildLock(root string) (func(), error) {
	lockDir := filepath.Join(root, ".tu-agent")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("graph.Build: creating lock dir %s: %w", lockDir, err)
	}
	lockPath := filepath.Join(lockDir, "graph.build.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("graph.Build: opening lock file %s: %w", lockPath, err)
	}
	if err := flockExclusive(f.Fd()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("graph.Build: flock %s: %w", lockPath, err)
	}
	return func() {
		if err := flockRelease(f.Fd()); err != nil {
			slog.Warn("graph.Build: releasing single-flight lock", "path", lockPath, "err", err)
		}
		if err := f.Close(); err != nil {
			slog.Warn("graph.Build: closing single-flight lock file", "path", lockPath, "err", err)
		}
	}, nil
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

// anyParsed reports whether any known file record is a successful parse (status
// "ok"). It distinguishes a wiped node store (parsed files but zero nodes) from
// a repo that legitimately yields no nodes — files that failed to parse carry
// status "failed" and never contributed nodes, so they must not trip the guard.
func anyParsed(known map[string]store.FileRecord) bool {
	for _, r := range known {
		if r.Status == "ok" {
			return true
		}
	}
	return false
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
