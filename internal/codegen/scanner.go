package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectInfo holds information discovered about a codebase.
type ProjectInfo struct {
	Name        string   // project name from git remote or directory basename
	Root        string   // absolute path to the scanned root
	FileTypes   []string // sorted unique extensions found (e.g. ".go", ".java")
	FilePaths   []string // source file paths relative to Root, sorted
	TreeSummary string   // indented directory tree (up to 3 levels, dirs only)
}

// maxTreeLines limits the number of directory entries in TreeSummary to keep
// the prompt size bounded for large projects.
const maxTreeLines = 100

// ignoreDirs lists directory names that are always skipped during scanning.
var ignoreDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
	"target": true, "build": true, ".tu-agent": true,
	"__pycache__": true, ".idea": true, ".vscode": true,
	"dist": true, "out": true, ".gradle": true,
	".claude": true,
}

// Scan discovers source files under root matching the given extensions.
// Extensions must include the dot (e.g. ".go", ".java"). If exts is nil or
// empty, all files are collected. Directories listed in ignoreDirs are skipped.
//
// If subpath is non-empty, FilePaths is filtered to files under
// <root>/<subpath>; the subpath is resolved relative to root and may not
// escape it. TreeSummary still reflects the entire project so the model knows
// what else exists outside the focused area. Returned paths are always
// relative to root, never to subpath, so read_file calls remain valid.
//
// If recursive is false, FilePaths only includes files DIRECTLY in the
// subpath (no descent into child directories). Useful for the init-all
// chunked traversal where intermediate dirs need their own pass for files
// not covered by deeper chunks.
func Scan(root string, subpath string, recursive bool, exts []string) (*ProjectInfo, error) {
	return ScanWithExcludes(root, subpath, recursive, nil, exts)
}

// ScanWithExcludes is like Scan but additionally skips any path whose root-
// relative form starts with one of excludeSubpaths. Used by init-all to emit
// a recursive parent chunk that does not double-process subtrees already
// covered by their own deeper chunks.
func ScanWithExcludes(root string, subpath string, recursive bool, excludeSubpaths []string, exts []string) (*ProjectInfo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("codegen.Scan: resolving root: %w", err)
	}

	subpathClean := ""
	if subpath != "" {
		subAbs, err := filepath.Abs(filepath.Join(absRoot, subpath))
		if err != nil {
			return nil, fmt.Errorf("codegen.Scan: resolving subpath: %w", err)
		}
		if subAbs != absRoot && !strings.HasPrefix(subAbs, absRoot+string(filepath.Separator)) {
			return nil, fmt.Errorf("codegen.Scan: subpath %q escapes root %q", subpath, absRoot)
		}
		subpathClean, err = filepath.Rel(absRoot, subAbs)
		if err != nil {
			return nil, fmt.Errorf("codegen.Scan: computing relative subpath: %w", err)
		}
	}

	excludeSet := make([]string, 0, len(excludeSubpaths))
	for _, ex := range excludeSubpaths {
		ex = filepath.Clean(ex)
		if ex == "" || ex == "." {
			continue
		}
		excludeSet = append(excludeSet, ex)
	}

	extSet := make(map[string]bool, len(exts))
	for _, e := range exts {
		extSet[strings.ToLower(e)] = true
	}

	var paths []string
	typeSet := make(map[string]bool)
	var treeLines []string

	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil // skip unreadable entries
		}
		rel, _ := filepath.Rel(absRoot, path)
		if d.IsDir() {
			if ignoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			if rel == "." {
				return nil
			}
			depth := strings.Count(rel, string(filepath.Separator))
			if depth < 3 {
				indent := strings.Repeat("  ", depth)
				treeLines = append(treeLines, indent+d.Name()+"/")
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if len(extSet) > 0 && !extSet[ext] {
			return nil
		}
		if subpathClean != "" && subpathClean != "." {
			if rel != subpathClean && !strings.HasPrefix(rel, subpathClean+string(filepath.Separator)) {
				return nil
			}
		}
		for _, ex := range excludeSet {
			if rel == ex || strings.HasPrefix(rel, ex+string(filepath.Separator)) {
				return nil
			}
		}
		if !recursive {
			parent := subpathClean
			if parent == "" {
				parent = "."
			}
			if filepath.Dir(rel) != parent {
				return nil
			}
		}
		paths = append(paths, rel)
		typeSet[ext] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codegen.Scan: walking %s: %w", absRoot, err)
	}

	sort.Strings(paths)
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	treeSummary := buildTreeSummary(treeLines)

	return &ProjectInfo{
		Name:        ProjectName(absRoot),
		Root:        absRoot,
		FileTypes:   types,
		FilePaths:   paths,
		TreeSummary: treeSummary,
	}, nil
}

// buildTreeSummary joins directory lines and caps at maxTreeLines to keep
// prompt sizes bounded for large projects.
func buildTreeSummary(lines []string) string {
	if len(lines) <= maxTreeLines {
		return strings.Join(lines, "\n")
	}
	truncated := strings.Join(lines[:maxTreeLines], "\n")
	return fmt.Sprintf("%s\n... (%d more dirs not shown)", truncated, len(lines)-maxTreeLines)
}

// ProjectName returns the project name from the git remote URL (last path
// component, .git suffix stripped) or the directory basename as fallback.
func ProjectName(root string) string {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin") //nolint:gosec
	out, err := cmd.Output()
	if err == nil {
		url := strings.TrimSpace(string(out))
		url = strings.TrimSuffix(url, ".git")
		parts := strings.FieldsFunc(url, func(r rune) bool {
			return r == '/' || r == ':'
		})
		if len(parts) > 0 && parts[len(parts)-1] != "" {
			return parts[len(parts)-1]
		}
	}
	return filepath.Base(root)
}
