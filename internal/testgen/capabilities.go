package testgen

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DetectCapabilities returns generic, language-level test-infrastructure hints
// for the repo at root, so generation uses available techniques instead of
// rediscovering or avoiding them. Heuristic and advisory: it detects framework
// presence only, never project-specific patterns. Hints are sorted; an empty
// slice means nothing recognizable was found.
func DetectCapabilities(repoRoot, language string) []string {
	var hints []string
	switch language {
	case "java":
		// mockito-inline registers a MockMaker under test resources; its
		// presence means static mocking (mockStatic) works.
		_ = filepath.WalkDir(repoRoot, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(p) == "org.mockito.plugins.MockMaker" &&
				strings.Contains(filepath.ToSlash(p), "mockito-extensions/") {
				hints = append(hints, "mockStatic available (mockito-inline mock-maker registered)")
				return filepath.SkipAll
			}
			return nil
		})
	case "python":
		if fileContains(filepath.Join(repoRoot, "requirements.txt"), "pytest-mock") ||
			fileContains(filepath.Join(repoRoot, "pyproject.toml"), "pytest-mock") {
			hints = append(hints, "pytest-mock available (use the mocker fixture)")
		}
	}
	sort.Strings(hints)
	return hints
}

// fileContains reports whether the file at path exists and contains substr.
func fileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), substr)
}

// writeFileContent writes content to path, creating parent dirs. Test helper
// kept here so capabilities_test.go can seed fixture files with content.
func writeFileContent(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
