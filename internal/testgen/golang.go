package testgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GoAdapter implements LanguageAdapter for Go repos using the standard toolchain.
type GoAdapter struct{}

func (a *GoAdapter) Detect(repoRoot string) error {
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		return fmt.Errorf("GoAdapter.Detect: no go.mod at %s — cannot verify, use --dry-run", repoRoot)
	}
	return nil
}

func (a *GoAdapter) TestPath(repoRoot string, t Target) (string, error) {
	return strings.TrimSuffix(t.Path, ".go") + "_test.go", nil
}

func (a *GoAdapter) PromptFragment(t Target, testPath string) string {
	prefix := goGenPrefix(t)
	return fmt.Sprintf(`Write a Go test file at %s.
Rules:
- Declare the same package as the source file (its package clause is in the context).
- Use only the standard library "testing" package; table-driven where natural.
- Every test function name MUST start with %q (e.g. func %sHappyPath(t *testing.T)).
- Output one complete compilable file: package clause, imports, tests. No explanations.`,
		testPath, prefix, prefix)
}

func (a *GoAdapter) RunCommand(repoRoot, testPath string, t Target) ([]string, error) {
	dir := filepath.ToSlash(filepath.Dir(testPath))
	return []string{"go", "test", "-run", "^" + goGenPrefix(t), "./" + dir}, nil
}
