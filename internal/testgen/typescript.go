package testgen

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TSAdapter implements LanguageAdapter for TypeScript repos using vitest or
// jest. It is workspace-aware: each target resolves to its nearest enclosing
// package, and framework / test-file convention / run directory are derived
// from that package (spec). resolve() memoizes per target path; a single
// TSAdapter instance handles one target per BuildScaffold, but the map keeps it
// correct under reuse.
type TSAdapter struct {
	cache map[string]tsResolved
}

// tsResolved is the cached per-target resolution.
type tsResolved struct {
	pkgDir    string // repo-relative; "." = root
	framework string // "vitest" | "jest"; always set (for prompt/run)
	declared  bool   // a vitest/jest declaration was found on the walk-up chain
	suffix    string // ".spec" | ".test"
}

// resolve computes and memoizes the target's package resolution.
func (a *TSAdapter) resolve(repoRoot string, t Target) (tsResolved, error) {
	if a.cache == nil {
		a.cache = map[string]tsResolved{}
	}
	if r, ok := a.cache[t.Path]; ok {
		return r, nil
	}
	pkgDir := nearestPackageDir(repoRoot, t.Path)
	suffix := tsConventionSuffix(repoRoot, pkgDir)
	fw, declared := tsFramework(repoRoot, pkgDir, suffix)
	r := tsResolved{pkgDir: pkgDir, framework: fw, declared: declared, suffix: suffix}
	a.cache[t.Path] = r
	return r, nil
}

// tsConventionSuffix counts *.spec.ts(x) vs *.test.ts(x) under pkgDir (skipping
// node_modules) and returns the majority suffix; a tie or no test files yields
// ".test" (the historical default).
func tsConventionSuffix(repoRoot, pkgDir string) string {
	spec, test := 0, 0
	base := filepath.Join(repoRoot, pkgDir)
	_ = filepath.WalkDir(base, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".spec.ts"), strings.HasSuffix(name, ".spec.tsx"):
			spec++
		case strings.HasSuffix(name, ".test.ts"), strings.HasSuffix(name, ".test.tsx"):
			test++
		}
		return nil
	})
	if spec > test {
		return ".spec"
	}
	return ".test"
}

// tsFramework walks up from pkgDir to the repo root; the first package.json
// declaring vitest or jest wins. A level declaring BOTH (or no declaration
// anywhere) is broken by convention: ".spec" -> jest, else vitest. declared is
// false only when no level declared either, in which case framework defaults to
// the convention-matched one for the prompt/run.
func tsFramework(repoRoot, pkgDir, suffix string) (framework string, declared bool) {
	dir := pkgDir
	for {
		v, j := tsDeclares(repoRoot, dir)
		switch {
		case v && j:
			if suffix == ".spec" {
				return "jest", true
			}
			return "vitest", true
		case v:
			return "vitest", true
		case j:
			return "jest", true
		}
		if dir == "." {
			break
		}
		dir = parentDir(dir)
	}
	if suffix == ".spec" {
		return "jest", false
	}
	return "vitest", false
}

// tsDeclares reports whether repoRoot/dir/package.json lists vitest / jest in
// dependencies or devDependencies. A missing or unparseable file yields false,
// false.
func tsDeclares(repoRoot, dir string) (vitest, jest bool) {
	data, err := os.ReadFile(filepath.Join(repoRoot, dir, "package.json"))
	if err != nil {
		return false, false
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, false
	}
	has := func(name string) bool {
		if _, ok := pkg.Dependencies[name]; ok {
			return true
		}
		_, ok := pkg.DevDependencies[name]
		return ok
	}
	return has("vitest"), has("jest")
}

// parentDir returns dir's parent, collapsing to "." at the top.
func parentDir(dir string) string {
	p := filepath.Dir(dir)
	if p == dir {
		return "."
	}
	return p
}

// pkgRelative makes a repo-relative testPath relative to pkgDir (the run cwd).
func pkgRelative(pkgDir, testPath string) string {
	if pkgDir == "." {
		return testPath
	}
	return strings.TrimPrefix(testPath, pkgDir+"/")
}

// Detect is a repo-level smoke check: nil when the repo root has a package.json.
// Per-target framework precision happens in resolve() at scaffold time.
func (a *TSAdapter) Detect(repoRoot string) error {
	if _, err := os.Stat(filepath.Join(repoRoot, "package.json")); err != nil {
		return fmt.Errorf("TSAdapter.Detect: no package.json at %s — not an npm project, use --dry-run", repoRoot)
	}
	return nil
}

func (a *TSAdapter) TestPath(repoRoot string, t Target) (string, error) {
	r, err := a.resolve(repoRoot, t)
	if err != nil {
		return "", err
	}
	ext := ".ts"
	if strings.HasSuffix(strings.ToLower(t.Path), ".tsx") {
		ext = ".tsx"
	}
	return t.Path[:len(t.Path)-len(ext)] + r.suffix + ext, nil
}

func (a *TSAdapter) PromptFragment(t Target, testPath string) string {
	// PromptFragment has no repoRoot; the framework was resolved by the prior
	// TestPath/RunCommand call (BuildScaffold calls TestPath then RunCommand
	// before PromptFragment) and cached. Fall back to vitest if unset.
	fw := "vitest"
	if r, ok := a.cache[t.Path]; ok {
		fw = r.framework
	}
	importRule := `Import describe/it/expect from "vitest" explicitly.`
	if fw == "jest" {
		importRule = "Use the jest globals (describe/it/expect); do not import them."
	}
	return fmt.Sprintf(`Write a %s test file at %s.
Rules:
- %s
- Put ALL generated tests inside a single describe block titled exactly %q.
- Wrap that describe block between a line "// tu-agent:gen:start" and a line "// tu-agent:gen:end".
- Import the module under test by relative path, exactly as the call sites in the context do.
- Output one complete runnable file: imports first, then the wrapped describe. No explanations.`,
		fw, testPath, importRule, tsGenTitle(t))
}

func (a *TSAdapter) RunCommand(repoRoot, testPath string, t Target) ([]string, error) {
	r, err := a.resolve(repoRoot, t)
	if err != nil {
		return nil, err
	}
	rel := pkgRelative(r.pkgDir, testPath)
	switch r.framework {
	case "vitest":
		return []string{"npx", "vitest", "run", rel, "-t", tsGenRunPattern(t)}, nil
	case "jest":
		return []string{"npx", "jest", rel, "-t", tsGenRunPattern(t)}, nil
	}
	return nil, fmt.Errorf("TSAdapter.RunCommand: no framework resolved at %s", repoRoot)
}

// runDir is the optional package-run-directory hook (Task 3): the pipeline runs
// the scoped test here. Returns the target's package dir (repo-relative).
func (a *TSAdapter) runDir(repoRoot string, t Target) string {
	r, err := a.resolve(repoRoot, t)
	if err != nil {
		return "."
	}
	return r.pkgDir
}

// ResolveForCoverage exposes the target's package dir and framework for the CLI
// coverage path. It never errors (resolve does not); on the zero case it
// returns ".","vitest".
func (a *TSAdapter) ResolveForCoverage(repoRoot string, t Target) (pkgDir, framework string) {
	r, err := a.resolve(repoRoot, t)
	if err != nil {
		return ".", "vitest"
	}
	return r.pkgDir, r.framework
}
