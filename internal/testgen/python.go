package testgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// PythonAdapter implements LanguageAdapter for Python repos using pytest.
// pytest is assumed installed in the user's environment;
// if it is not, the run fails and distills like any other runner error.
type PythonAdapter struct{}

// pythonMarkers are the root files that mark a Python project for Detect.
var pythonMarkers = []string{"pyproject.toml", "setup.py", "setup.cfg", "pytest.ini", "requirements.txt"}

func (a *PythonAdapter) Detect(repoRoot string) error {
	for _, m := range pythonMarkers {
		if _, err := os.Stat(filepath.Join(repoRoot, m)); err == nil {
			return nil
		}
	}
	return fmt.Errorf("PythonAdapter.Detect: no Python project marker (%s) at %s — cannot verify, use --dry-run",
		strings.Join(pythonMarkers, ", "), repoRoot)
}

func (a *PythonAdapter) TestPath(repoRoot string, t Target) (string, error) {
	module := strings.TrimSuffix(filepath.Base(t.Path), ".py")
	dir := filepath.ToSlash(filepath.Dir(t.Path))
	conventional := filepath.ToSlash(filepath.Join(dir, "test_"+module+".py"))
	if st, err := os.Stat(filepath.Join(repoRoot, "tests")); err == nil && st.IsDir() {
		rel := dir
		if rel == "src" || rel == "." {
			rel = ""
		}
		rel = strings.TrimPrefix(rel, "src/")
		conventional = filepath.ToSlash(filepath.Join("tests", rel, "test_"+module+".py"))
	}
	return conventional, nil
}

func (a *PythonAdapter) PromptFragment(t Target, testPath string) string {
	prefix := pyGenPrefix(t)
	return fmt.Sprintf(`Write a pytest test file at %s.
Rules:
- Plain pytest: top-level test functions and bare assert statements; no unittest classes.
- Every test function name MUST start with %q (e.g. def %s_happy_path()).
- Wrap ALL generated test functions between a line "# tu-agent:gen:start" and a line "# tu-agent:gen:end".
- Use @pytest.mark.parametrize where a table of cases is natural.
- Cover real branches and error paths, not just happy-path returns: use monkeypatch or unittest.mock (patch, MagicMock) to stub collaborators and reach conditionals and error handling.
- Import the module under test the same way the call sites in the context do.
- Output one complete runnable file: imports first, then the wrapped tests. No explanations.`,
		testPath, prefix, prefix)
}

func (a *PythonAdapter) RunCommand(repoRoot, testPath string, t Target) ([]string, error) {
	return []string{"python3", "-m", "pytest", testPath, "-k", pyGenStem(t), "-q"}, nil
}

// snakeCase lowers a symbol name to snake_case for pytest function names:
// "Store.Save" → "store_save", "HTTPServer" → "http_server".
func snakeCase(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		switch {
		case r == '.':
			b.WriteRune('_')
		case unicode.IsUpper(r):
			prevLower := i > 0 && unicode.IsLower(rs[i-1])
			prevUpper := i > 0 && unicode.IsUpper(rs[i-1])
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if prevLower || (prevUpper && nextLower) {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
