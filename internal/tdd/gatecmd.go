package tdd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/carlosneir4/tu-agent/internal/config"
	"github.com/carlosneir4/tu-agent/internal/testresult"
)

// GateResult is the JSON the gate prints for the plugin skill to read.
type GateResult struct {
	OK       bool   `json:"ok"`
	Feedback string `json:"feedback,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

// redBaseline is the durable anti-cheat record the RED gate writes and the
// GREEN gate verifies: the exact content hashes of the new test files at the
// moment they were proven red. Survives conductor context compaction.
type redBaseline struct {
	Feature string            `json:"feature"`
	Files   map[string]string `json:"files"`
}

// baselinePath is where the RED baseline lives for a given per-feature base dir.
func baselinePath(base string) string {
	return filepath.Join(base, "progress", "red-baseline.json")
}

// sha256OfFile returns the hex-encoded sha256 of a file's content.
func sha256OfFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("sha256OfFile: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// writeRedBaseline records the content hash of each new test file once the
// RED gate confirms they are failing, so a later GREEN gate can detect the
// tests being weakened rather than the production code being fixed.
func writeRedBaseline(base, feature string, root string, newTests []string) error {
	bl := redBaseline{Feature: feature, Files: map[string]string{}}
	for _, rel := range newTests {
		h, err := sha256OfFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("writeRedBaseline: hashing %s: %w", rel, err)
		}
		bl.Files[rel] = h
	}
	if err := os.MkdirAll(filepath.Join(base, "progress"), 0o755); err != nil {
		return fmt.Errorf("writeRedBaseline: %w", err)
	}
	data, err := json.MarshalIndent(bl, "", "  ")
	if err != nil {
		return fmt.Errorf("writeRedBaseline: %w", err)
	}
	if err := os.WriteFile(baselinePath(base), data, 0o644); err != nil {
		return fmt.Errorf("writeRedBaseline: %w", err)
	}
	return nil
}

// checkRedBaseline returns feedback for the GREEN gate's anti-cheat guard:
// empty means the RED baseline is present, matches this feature, and no
// baselined test file has been mutated since RED — the gate may pass.
// Non-empty means the guard could not be satisfied, either because a
// baselined test file was mutated after RED, or because the baseline is
// missing/unusable for this feature. Both cases are gate failures: skipping
// RED must not be a silent path to a GREEN pass.
func checkRedBaseline(base, feature, root string) string {
	data, err := os.ReadFile(baselinePath(base))
	if err != nil {
		return "no red baseline found for this feature — run `tdd gate --expect red` first"
	}
	var bl redBaseline
	if err := json.Unmarshal(data, &bl); err != nil || bl.Feature != feature {
		return "red baseline unusable for this feature — run `tdd gate --expect red` first"
	}
	var mutated []string
	for rel, want := range bl.Files {
		got, err := sha256OfFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || got != want {
			mutated = append(mutated, rel)
		}
	}
	if len(mutated) > 0 {
		sort.Strings(mutated)
		return "test files mutated since RED: " + strings.Join(mutated, ", ")
	}
	return ""
}

// splitTags splits a comma-separated tag list, trimming spaces and dropping empties.
func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// evalRed adapts NewTestsRed into the gate result shape.
func evalRed(overallPassed bool, rep testresult.Report, newTests []string) GateResult {
	r := NewTestsRed(overallPassed, rep, newTests)
	return GateResult{OK: r.OK, Feedback: r.Feedback}
}

// goTestFuncRe matches top-level Go test function declarations.
var goTestFuncRe = regexp.MustCompile(`(?m)^func (Test\w+)\s*\(`)

// goNewTestsRed proves each new Go test file red by running exactly its Test
// functions scoped to the file's package. JUnit reports do not exist for plain
// `go test`, so this is the per-file RED check for Go repos. Non-empty return =
// violation feedback. A genuine test FAILURE at runtime counts as red; a
// BUILD/COMPILE failure of the scoped package does not — it is not a
// legitimately-failing test.
//
// buildTags carries config's tdd.build_tags. This scoped run is a second way to
// compile the project, next to TestCommand, so it must be told how the project
// builds: without the tags it produces a DIFFERENT program, and code that only
// exists under a tag cannot fail there. The verdict then inverts — a genuinely
// red test reads as green.
func goNewTestsRed(ctx context.Context, root string, goFiles []string, buildTags []string) string {
	for _, rel := range goFiles {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Sprintf("cannot read new test file %s: %v", rel, err)
		}
		m := goTestFuncRe.FindAllStringSubmatch(string(src), -1)
		if len(m) == 0 {
			return fmt.Sprintf("new test file %s declares no Test functions", rel)
		}
		names := make([]string, 0, len(m))
		for _, g := range m {
			names = append(names, g[1])
		}
		pkgDir := "./" + path.Dir(rel)
		args := []string{"test", "-count=1"}
		if len(buildTags) > 0 {
			args = append(args, "-tags", strings.Join(buildTags, ","))
		}
		args = append(args, "-run", "^("+strings.Join(names, "|")+")$", pkgDir)
		c := exec.CommandContext(ctx, "go", args...)
		c.Dir = root
		out, err := c.CombinedOutput()
		if err == nil {
			if strings.Contains(string(out), "no tests to run") {
				return fmt.Sprintf("new test file %s matched no tests — scoped run must fail, not report zero matches", rel)
			}
			// With no tags configured we cannot tell a test that is genuinely
			// green from one that is red only under the project's real build, so
			// name both rather than assert the first.
			if len(buildTags) == 0 {
				return fmt.Sprintf("new test file %s passes — either it needs a failing assertion first, "+
					"or this repo builds with tags that tdd.build_tags in .tu-agent/config.yaml does not declare", rel)
			}
			return fmt.Sprintf("new test file %s passes (built with -tags %s) — tests must fail before implementation "+
				"(write a failing assertion first)", rel, strings.Join(buildTags, ","))
		}
		if strings.Contains(string(out), "build constraints exclude all Go files") || strings.Contains(string(out), "no Go files in") {
			return fmt.Sprintf("new test file %s is excluded by build constraints — tests must compile and fail, not be skipped", rel)
		}
		if strings.Contains(string(out), "[build failed]") || strings.Contains(string(out), "[setup failed]") {
			return fmt.Sprintf("new test file %s fails to build — it must compile and fail at runtime, not fail to build:\n%s", rel, strings.TrimSpace(string(out)))
		}
	}
	return ""
}

// RunGate reads the feature's required @s tags, runs the deterministic gate
// (coverage + green tests) via the same functions the CLI conductor uses, and
// returns the structured result. A missing feature file or unresolved test
// command is an error (the caller distinguishes "ran and failed" from "could
// not run"). The test runner is resolved through the injected resolveRunner so
// this package never reaches back into package main.
func RunGate(ctx context.Context, cfg config.Config, root, ticket, feature, coveredRaw, expect, newTestsRaw, gateBase string, resolveRunner func(config.Config, string) (TestRunner, error)) (GateResult, error) {
	if feature == "" {
		return GateResult{}, fmt.Errorf("--feature is required")
	}
	// Validate expect: must be "red" or "green" (empty string defaults to "green")
	if expect == "" {
		expect = "green"
	}
	if expect != "red" && expect != "green" {
		return GateResult{}, fmt.Errorf("--expect must be red or green, got %q", expect)
	}

	// Resolve the per-feature base dir once: explicit --base wins (relative
	// paths join to root), else fall back to ticket/mtime resolution, else the
	// legacy flat layout.
	base := gateBase
	if base == "" {
		if b, ok := ResolveTddBaseForFeature(root, ticket, feature); ok {
			base = b
		} else {
			base = filepath.Join(root, ".tu-agent", "tdd")
		}
	} else if !filepath.IsAbs(base) {
		base = filepath.Join(root, filepath.FromSlash(base))
	}

	if expect == "red" {
		runner, err := resolveRunner(cfg, root)
		if err != nil {
			return GateResult{}, err
		}
		since := time.Now()
		passed, _, rerr := runner(ctx)
		if rerr != nil {
			return GateResult{}, fmt.Errorf("test runner error: %w", rerr)
		}
		rep, lerr := testresult.LoadReports(root, since)
		if lerr != nil {
			return GateResult{}, fmt.Errorf("loading reports: %w", lerr)
		}
		newTests := splitTags(newTestsRaw)
		goScopedConfirmed := false
		allNewTestsAreGo := false
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			var goFiles []string
			for _, rel := range newTests {
				if strings.HasSuffix(rel, ".go") {
					goFiles = append(goFiles, rel)
				}
			}
			if len(goFiles) > 0 {
				if fb := goNewTestsRed(ctx, root, goFiles, cfg.Tdd.BuildTags); fb != "" {
					return GateResult{OK: false, Feedback: fb}, nil
				}
				goScopedConfirmed = true
				allNewTestsAreGo = len(goFiles) == len(newTests)
			}
		}
		// A confirmed per-file scoped red proof (Go repos) is sufficient on its
		// own ONLY when every new-test file is Go: evalRed's suite-level verdict
		// must not override it (a green overall suite with no JUnit reports
		// would otherwise overturn a genuinely red new test —
		// bug-pattern/red-gate-suite-scope-override). In a polyglot feature with
		// both Go and non-Go (e.g. JUnit) new tests, the Go per-file proof only
		// covers the Go files — the non-Go files must still be proven red via
		// evalRed, or a JUnit test that is green-on-arrival would be silently
		// baselined just because a sibling Go test was red.
		var res GateResult
		if goScopedConfirmed && allNewTestsAreGo {
			res = GateResult{OK: true}
		} else {
			res = evalRed(passed, rep, newTests)
		}
		if res.OK {
			if err := writeRedBaseline(base, feature, root, newTests); err != nil {
				return GateResult{}, fmt.Errorf("runGate: %w", err)
			}
		}
		return res, nil
	}
	// expect green: read feature file from the per-feature dir
	featPath := filepath.Join(base, "features", feature+".feature")
	data, err := os.ReadFile(featPath)
	if err != nil {
		return GateResult{}, fmt.Errorf("reading feature: %w", err)
	}
	if fb := checkRedBaseline(base, feature, root); fb != "" {
		return GateResult{OK: false, Feedback: fb}, nil
	}
	runner, err := resolveRunner(cfg, root)
	if err != nil {
		return GateResult{}, err
	}
	required := ScenarioTags(string(data))
	det := DeterministicJudge(ctx, runner, required, splitTags(coveredRaw))
	return GateResult{OK: det.OK, Feedback: det.Feedback}, nil
}
