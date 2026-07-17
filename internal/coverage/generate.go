package coverage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes argv in repoRoot and returns combined output. Injectable so
// tests never shell out.
type Runner func(repoRoot string, argv []string) (string, error)

// ExecRunner is the default Runner.
func ExecRunner(repoRoot string, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("coverage.ExecRunner: empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("coverage.ExecRunner: %s: %w", strings.Join(argv, " "), err)
	}
	return string(out), nil
}

// coverageDir returns the project-local directory where coverage reports are
// written under repoRoot. It is the authoritative definition of that path:
// this package cannot import cmd, so the project layout for it lives here.
func coverageDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".tu-agent")
}

// Generate runs the language's test suite with coverage and parses the result.
// On any failure (missing tool, failing suite, unreadable report) it returns a
// wrapped error the caller logs before falling back to the graph proxy.
func Generate(lang, repoRoot, modulePath string, run Runner) (Profile, error) {
	dir := coverageDir(repoRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("coverage.Generate: %w", err)
	}
	switch lang {
	case LangGo:
		out := filepath.Join(dir, "coverage.out")
		if o, err := run(repoRoot, []string{"go", "test", "-coverprofile=" + out, "./..."}); err != nil {
			return nil, fmt.Errorf("coverage.Generate(go): %s: %w", strings.TrimSpace(o), err)
		}
		return loadFile(LangGo, out, modulePath, repoRoot)
	case LangPython:
		if o, err := run(repoRoot, []string{"coverage", "run", "-m", "pytest"}); err != nil {
			return nil, fmt.Errorf("coverage.Generate(python) run: %s: %w", strings.TrimSpace(o), err)
		}
		out := filepath.Join(dir, "coverage.xml")
		if o, err := run(repoRoot, []string{"coverage", "xml", "-o", out}); err != nil {
			return nil, fmt.Errorf("coverage.Generate(python) xml: %s: %w", strings.TrimSpace(o), err)
		}
		return loadFile(LangPython, out, modulePath, repoRoot)
	case LangJava:
		if o, err := run(repoRoot, []string{mavenBin(repoRoot), "-q", "test"}); err != nil {
			return nil, fmt.Errorf("coverage.Generate(java): %s: %w", strings.TrimSpace(o), err)
		}
		return loadFile(LangJava, filepath.Join(repoRoot, "target", "site", "jacoco", "jacoco.xml"), modulePath, repoRoot)
	case LangTS:
		// modulePath is unused for TypeScript; Istanbul reports carry file paths, not a module path.
		return GenerateTS(repoRoot, ".", "vitest", run)
	default:
		return nil, fmt.Errorf("coverage.Generate: no coverage runner for %q", lang)
	}
}

// mavenBin prefers the wrapper.
func mavenBin(repoRoot string) string {
	if _, err := os.Stat(filepath.Join(repoRoot, "mvnw")); err == nil {
		return "./mvnw"
	}
	return "mvn"
}

// GenerateTS runs the package's test suite with coverage in repoRoot/pkgDir and
// parses the Istanbul coverage-final.json it emits. framework is "vitest" or
// "jest"; both write Istanbul JSON. Paths in the report are made repo-relative
// against repoRoot by ParseIstanbul, matching the graph's repo-relative paths.
func GenerateTS(repoRoot, pkgDir, framework string, run Runner) (Profile, error) {
	workdir := filepath.Join(repoRoot, pkgDir)
	var argv []string
	switch framework {
	case "jest":
		argv = []string{"npx", "jest", "--coverage"}
	default: // vitest
		argv = []string{"npx", "vitest", "run", "--coverage"}
	}
	if o, err := run(workdir, argv); err != nil {
		return nil, fmt.Errorf("coverage.GenerateTS(%s): %s: %w", framework, strings.TrimSpace(o), err)
	}
	return loadFile(LangTS, filepath.Join(workdir, "coverage", "coverage-final.json"), "", repoRoot)
}

// loadFile opens path and parses it for lang.
func loadFile(lang, path, modulePath, repoRoot string) (Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("coverage.Generate: reading report: %w", err)
	}
	defer f.Close()
	return parseFor(lang, f, modulePath, repoRoot)
}
