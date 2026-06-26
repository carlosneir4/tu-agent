package codegen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

// mkFixture creates a small fake codebase for scanner tests.
func mkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"main.go":              "package main\nfunc main() {}",
		"handler/user.go":      "package handler\ntype UserHandler struct{}",
		"handler/product.go":   "package handler\ntype ProductHandler struct{}",
		"storage/db.go":        "package storage\ntype DB struct{}",
		"README.md":            "# sample-svc",
		"vendor/lib/ignore.go": "package lib", // must be ignored
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestScan_FiltersByExtension(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	for _, p := range info.FilePaths {
		if filepath.Ext(p) != ".go" {
			t.Errorf("expected only .go files, got %q", p)
		}
	}
	// README.md should not appear
	for _, p := range info.FilePaths {
		if filepath.Base(p) == "README.md" {
			t.Error("README.md should be excluded when filtering .go")
		}
	}
}

func TestScan_IgnoresVendorDir(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	for _, p := range info.FilePaths {
		if len(p) >= 6 && p[:6] == "vendor" {
			t.Errorf("vendor file should be excluded: %q", p)
		}
	}
}

func TestScan_ReturnsFileTypes(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go", ".md"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	typeSet := make(map[string]bool)
	for _, ft := range info.FileTypes {
		typeSet[ft] = true
	}
	if !typeSet[".go"] {
		t.Error("expected .go in FileTypes")
	}
	if !typeSet[".md"] {
		t.Error("expected .md in FileTypes")
	}
}

func TestScan_RootIsAbsolute(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if !filepath.IsAbs(info.Root) {
		t.Errorf("Root should be absolute, got %q", info.Root)
	}
}

func TestScan_FilePathsAreSorted(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	for i := 1; i < len(info.FilePaths); i++ {
		if info.FilePaths[i] < info.FilePaths[i-1] {
			t.Errorf("FilePaths not sorted at index %d: %q < %q",
				i, info.FilePaths[i], info.FilePaths[i-1])
		}
	}
}

func TestScan_TreeSummaryNonEmpty(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if info.TreeSummary == "" {
		t.Error("TreeSummary should be non-empty for a codebase with subdirectories")
	}
}

func TestProjectName_FallbackToDir(t *testing.T) {
	root := t.TempDir()
	// TempDir path ends with a generated name — use a subdirectory with a known name
	named := filepath.Join(root, "my-service")
	if err := os.MkdirAll(named, 0o755); err != nil {
		t.Fatal(err)
	}
	name := codegen.ProjectName(named)
	if name != "my-service" {
		t.Errorf("expected 'my-service', got %q", name)
	}
}

func TestScan_EmptyPatternsReturnsAll(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "", true, nil)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(info.FilePaths) == 0 {
		t.Error("empty patterns should return all files")
	}
}

func TestScan_SubpathFiltersFilePaths(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "handler", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(info.FilePaths) == 0 {
		t.Fatal("expected files under handler/")
	}
	for _, p := range info.FilePaths {
		if !strings.HasPrefix(p, "handler"+string(filepath.Separator)) {
			t.Errorf("expected path under handler/, got %q", p)
		}
	}
}

func TestScan_SubpathReturnsPathsRelativeToRoot(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "handler", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	for _, p := range info.FilePaths {
		// Paths must be root-relative (start with "handler/"), not subpath-relative (just filename).
		if filepath.IsAbs(p) {
			t.Errorf("path should be relative to root, got absolute %q", p)
		}
		if !strings.Contains(p, string(filepath.Separator)) {
			t.Errorf("expected root-relative path with separator, got %q", p)
		}
	}
}

func TestScan_EmptySubpathScansEverything(t *testing.T) {
	root := mkFixture(t)
	withSub, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(withSub.FilePaths) < 3 {
		t.Errorf("empty subpath should scan all files, got %d", len(withSub.FilePaths))
	}
}

func TestScan_SubpathTreeSummaryStillShowsFullProject(t *testing.T) {
	root := mkFixture(t)
	info, err := codegen.Scan(root, "handler", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	// TreeSummary should reveal sibling directories the model can't deep-read,
	// so the model knows what else exists in the project.
	if !strings.Contains(info.TreeSummary, "storage") {
		t.Error("TreeSummary should show sibling dirs (e.g. storage/) outside subpath")
	}
}

func TestScan_SubpathOutsideRootReturnsError(t *testing.T) {
	root := mkFixture(t)
	_, err := codegen.Scan(root, "../escape", true, []string{".go"})
	if err == nil {
		t.Fatal("expected error when subpath escapes root")
	}
}

func TestScan_ExcludeSubpaths_RemovesNestedDirs(t *testing.T) {
	root := mkFixture(t)
	// handler/ and storage/ exist. Recursive scan that excludes handler/ returns
	// main.go and storage/db.go but no handler/* files.
	info, err := codegen.ScanWithExcludes(root, "", true, []string{"handler"}, []string{".go"})
	if err != nil {
		t.Fatalf("ScanWithExcludes error: %v", err)
	}
	for _, p := range info.FilePaths {
		if strings.HasPrefix(p, "handler"+string(filepath.Separator)) {
			t.Errorf("excluded subpath should not appear in FilePaths, got %q", p)
		}
	}
	wantStorage := false
	for _, p := range info.FilePaths {
		if strings.HasPrefix(p, "storage"+string(filepath.Separator)) {
			wantStorage = true
		}
	}
	if !wantStorage {
		t.Error("non-excluded subdirs should still appear")
	}
}

func TestScan_ExcludeSubpaths_NilBehavesLikeScan(t *testing.T) {
	root := mkFixture(t)
	a, _ := codegen.Scan(root, "", true, []string{".go"})
	b, _ := codegen.ScanWithExcludes(root, "", true, nil, []string{".go"})
	if len(a.FilePaths) != len(b.FilePaths) {
		t.Errorf("nil excludes should match Scan output: %d vs %d", len(a.FilePaths), len(b.FilePaths))
	}
}

func TestScan_NonRecursive_OnlyDirectFiles(t *testing.T) {
	// Create a dir with files at depth 0 AND a child dir with files at depth 1.
	root := t.TempDir()
	for _, rel := range []string{"a.go", "b.go", "sub/c.go", "sub/deep/d.go"} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	info, err := codegen.Scan(root, "", false, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	got := map[string]bool{}
	for _, p := range info.FilePaths {
		got[p] = true
	}
	if !got["a.go"] || !got["b.go"] {
		t.Errorf("expected direct files a.go and b.go, got %v", info.FilePaths)
	}
	if got[filepath.Join("sub", "c.go")] || got[filepath.Join("sub", "deep", "d.go")] {
		t.Errorf("non-recursive scan should exclude files in subdirs, got %v", info.FilePaths)
	}
}

func TestScan_NonRecursive_WithSubpath(t *testing.T) {
	root := mkFixture(t)
	// handler/ has user.go and product.go directly. Non-recursive subpath=handler returns those.
	info, err := codegen.Scan(root, "handler", false, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(info.FilePaths) == 0 {
		t.Fatal("expected handler/ files")
	}
	for _, p := range info.FilePaths {
		if !strings.HasPrefix(p, "handler"+string(filepath.Separator)) {
			t.Errorf("expected path under handler/, got %q", p)
		}
		// must be direct: only one separator (handler/file.go), no deeper
		if strings.Count(p, string(filepath.Separator)) > 1 {
			t.Errorf("non-recursive scan returned nested path %q", p)
		}
	}
}

func TestScan_TreeSummaryCappedAt100Lines(t *testing.T) {
	root := t.TempDir()
	// Create 120 dirs at depth 0 — all within the scanner's depth limit.
	for i := 0; i < 120; i++ {
		dir := filepath.Join(root, fmt.Sprintf("module-%03d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A file is required so WalkDir visits the directory.
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info, err := codegen.Scan(root, "", true, []string{".go"})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(info.TreeSummary), "\n")
	// Capped body (100) + truncation message line = 101 lines max.
	if len(lines) > 101 {
		t.Errorf("TreeSummary should be capped at 101 lines (100 dirs + truncation), got %d", len(lines))
	}
	if !strings.Contains(info.TreeSummary, "more dirs") {
		t.Error("expected a truncation message when tree exceeds cap")
	}
}
