package codegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// packageJSON captures the few fields of package.json that codegen reads.
type packageJSON struct {
	PackageManager string            `json:"packageManager"`
	Scripts        map[string]string `json:"scripts"`
}

// readPackageJSON parses root/package.json. It returns a zero value (not an
// error) when the file is missing or malformed, so callers can treat a JS repo
// with an unreadable manifest the same as one with no scripts.
func readPackageJSON(root string) packageJSON {
	var pkg packageJSON
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return pkg
	}
	_ = json.Unmarshal(data, &pkg)
	return pkg
}

// DetectBuildTool inspects root-level files to identify the project's build tool.
// A committed wrapper script (gradlew/mvnw) is the strongest signal of which tool
// the team actually uses and wins over a bare pom.xml/build.gradle — a repo may
// carry both for legacy reasons while building with only one. For JS repos a bun
// lockfile or a `packageManager: "bun@..."` field beats the bare package.json
// (which would otherwise read as npm).
// Precedence (highest to lowest): gradlew > mvnw > pom.xml(maven) > build.gradle(gradle)
// > bun > pnpm > yarn > npm > pyproject > pip > make > go > unknown.
func DetectBuildTool(root string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	switch {
	case exists("gradlew"):
		return "gradle"
	case exists("mvnw"):
		return "maven"
	case exists("pom.xml"):
		return "maven"
	case exists("build.gradle") || exists("build.gradle.kts"):
		return "gradle"
	case exists("package.json") && (exists("bun.lockb") || exists("bun.lock")):
		return "bun"
	case exists("package.json") && strings.HasPrefix(readPackageJSON(root).PackageManager, "bun@"):
		return "bun"
	case exists("package.json") && exists("pnpm-lock.yaml"):
		return "pnpm"
	case exists("package.json") && exists("yarn.lock"):
		return "yarn"
	case exists("package.json"):
		return "npm"
	case exists("pyproject.toml"):
		return "pyproject"
	case exists("requirements.txt"):
		return "pip"
	case exists("Makefile"):
		return "make"
	case exists("go.mod"):
		return "go"
	default:
		return "unknown"
	}
}

// DetectTestCommand returns the standard test command for the given build tool.
// Returns "" when the build tool is unknown.
func DetectTestCommand(buildTool string) string {
	switch buildTool {
	case "maven":
		return "mvn test"
	case "gradle":
		return "./gradlew test"
	case "npm":
		return "npm test"
	case "yarn":
		return "yarn test"
	case "pnpm":
		return "pnpm test"
	case "bun":
		return "bun test"
	case "pyproject", "pip":
		return "pytest"
	case "make":
		return "make test"
	case "go":
		return "go test ./..."
	default:
		return ""
	}
}

// DetectTestCommandForRoot resolves the real test command for the project at
// root. It anchors on the detected build tool, and for JS package managers it
// reads package.json: a root `test` script means the canonical `<pm> test`
// invocation is valid. When no recognizable build tool is found it returns "".
//
// Monorepos that only define per-package scripts (e.g. `test:web`) and no root
// `test` script fall back to the package manager's default test runner — the
// agent still gets a real command, just not a package-scoped one.
func DetectTestCommandForRoot(root string) string {
	tool := DetectBuildTool(root)
	switch tool {
	case "npm", "yarn", "pnpm", "bun":
		if strings.TrimSpace(readPackageJSON(root).Scripts["test"]) != "" {
			return tool + " test"
		}
		return DetectTestCommand(tool)
	default:
		return DetectTestCommand(tool)
	}
}

// DetectPrimaryLanguage returns the language with the highest source file count.
// Returns "unknown" when no recognized source files are found.
func DetectPrimaryLanguage(filePaths []string) string {
	counts := map[string]int{}
	for _, p := range filePaths {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".java", ".kt", ".scala":
			counts["java"]++
		case ".ts", ".tsx", ".js", ".jsx":
			counts["typescript"]++
		case ".py":
			counts["python"]++
		case ".go":
			counts["go"]++
		}
	}
	best, bestCount := "unknown", 0
	for lang, n := range counts {
		if n > bestCount {
			bestCount = n
			best = lang
		}
	}
	return best
}
