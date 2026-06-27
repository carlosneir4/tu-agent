package testgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tu/tu-agent/internal/codegen"
)

// JavaAdapter implements LanguageAdapter for Maven and Gradle repos.
// Wrappers (./mvnw, ./gradlew) are preferred over global binaries.
type JavaAdapter struct{}

const (
	javaMain = "src/main/java/"
	javaTest = "src/test/java/"
)

func (a *JavaAdapter) Detect(repoRoot string) error {
	if a.runner(repoRoot) == "" {
		return fmt.Errorf("JavaAdapter.Detect: no pom.xml or build.gradle(.kts) at %s — cannot verify, use --dry-run", repoRoot)
	}
	return nil
}

// runner reports "maven", "gradle", or "" for the repo at root. It defers to
// codegen.DetectBuildTool so the wrapper-first precedence (a committed gradlew or
// mvnw wins over a bare pom.xml/build.gradle) is identical to the rest of
// tu-agent. Dual-build repos (which ship a pom.xml alongside a Gradle build, e.g.
// for legacy reasons) would otherwise be misread as Maven and yield an mvn
// run_command the project cannot run.
func (a *JavaAdapter) runner(repoRoot string) string {
	switch codegen.DetectBuildTool(repoRoot) {
	case "gradle":
		return "gradle"
	case "maven":
		return "maven"
	default:
		return ""
	}
}

func (a *JavaAdapter) TestPath(repoRoot string, t Target) (string, error) {
	if !strings.Contains(t.Path, javaMain) {
		return "", fmt.Errorf("JavaAdapter.TestPath: %s is not under %s — cannot derive a conventional test path", t.Path, javaMain)
	}
	base := strings.TrimSuffix(strings.Replace(t.Path, javaMain, javaTest, 1), ".java")
	return base + "Test.java", nil
}

func (a *JavaAdapter) PromptFragment(t Target, testPath string) string {
	cls := strings.TrimSuffix(filepath.Base(testPath), ".java")
	prefix := javaGenPrefix(t)
	return fmt.Sprintf(`Write a JUnit 5 test class at %s.
Rules:
- The public class MUST be named %s (it must match the file name) and declare the same package as the source class (its package clause is in the context).
- Use JUnit 5 (org.junit.jupiter.api). Aim to cover real branches and state, not just deterministic getters: use Mockito (mocks, spies, mockStatic) and test doubles freely to drive error paths, conditionals, and stateful behavior the context shows. Mock at collaborator boundaries rather than avoiding the logic under test.
- Every @Test method name MUST start with %q and continue in strict camelCase with NO underscores anywhere (Java's default checkstyle MethodName rule ^[a-z][a-zA-Z0-9]*$ rejects underscores — a test that compiles and passes can still fail the build), e.g. %sWhenEmpty(), never %s_when_empty().
- Inside the class body, wrap ALL generated @Test methods between a line "// tu-agent:gen:start" and a line "// tu-agent:gen:end".
- Output one complete compilable file: package clause, imports, class. No explanations.`,
		testPath, cls, prefix, prefix, prefix)
}

func (a *JavaAdapter) RunCommand(repoRoot, testPath string, t Target) ([]string, error) {
	cls := strings.TrimSuffix(filepath.Base(testPath), ".java")
	prefix := javaGenPrefix(t)
	switch a.runner(repoRoot) {
	case "maven":
		bin := "mvn"
		if _, err := os.Stat(filepath.Join(repoRoot, "mvnw")); err == nil {
			bin = "./mvnw"
		}
		return []string{bin, "-q", "test", "-Dtest=" + cls + "#" + prefix + "*"}, nil
	case "gradle":
		// Find javaTest anywhere in the path, not just as a prefix, so a
		// module-prefixed path (core/src/test/java/...) yields the package
		// FQCN, not core.src.test.java.* — mirrors TestPath's segment match.
		rel := filepath.ToSlash(testPath)
		if i := strings.LastIndex(rel, javaTest); i >= 0 {
			rel = rel[i+len(javaTest):]
		}
		fqcn := strings.ReplaceAll(strings.TrimSuffix(rel, ".java"), "/", ".")
		bin := "gradle"
		if _, err := os.Stat(filepath.Join(repoRoot, "gradlew")); err == nil {
			bin = "./gradlew"
		}
		return []string{bin, "test", "--tests", fqcn + "." + prefix + "*"}, nil
	}
	return nil, fmt.Errorf("JavaAdapter.RunCommand: no runner detected at %s", repoRoot)
}
