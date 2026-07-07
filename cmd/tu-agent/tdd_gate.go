package main

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

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/tdd"
	"github.com/tu/tu-agent/internal/testresult"
)

var (
	tddGateFeature  string
	tddGateCovered  string
	tddGateExpect   string
	tddGateNewTests string
	tddGateTicket   string
	tddGateBase     string
)

// tddGateResult is the JSON the gate prints for the plugin skill to read.
type tddGateResult struct {
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

// checkRedBaseline returns (feedback, warning): non-empty feedback means a
// baselined test file changed since RED (a gate failure); non-empty warning
// means the baseline is missing or unusable for this feature, so the guard
// is skipped rather than blocking the gate.
func checkRedBaseline(base, feature, root string) (string, string) {
	data, err := os.ReadFile(baselinePath(base))
	if err != nil {
		return "", "no red baseline found — test-mutation guard skipped"
	}
	var bl redBaseline
	if err := json.Unmarshal(data, &bl); err != nil || bl.Feature != feature {
		return "", "red baseline unusable for this feature — test-mutation guard skipped"
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
		return "test files mutated since RED: " + strings.Join(mutated, ", "), ""
	}
	return "", ""
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

// evalRed adapts tdd.NewTestsRed into the gate result shape.
func evalRed(overallPassed bool, rep testresult.Report, newTests []string) tddGateResult {
	r := tdd.NewTestsRed(overallPassed, rep, newTests)
	return tddGateResult{OK: r.OK, Feedback: r.Feedback}
}

// goTestFuncRe matches top-level Go test function declarations.
var goTestFuncRe = regexp.MustCompile(`(?m)^func (Test\w+)\s*\(`)

// goNewTestsRed proves each new Go test file red by running exactly its Test
// functions scoped to the file's package. JUnit reports do not exist for plain
// `go test`, so this is the per-file RED check for Go repos. Non-empty return =
// violation feedback; a build failure in the scoped run counts as red.
func goNewTestsRed(ctx context.Context, root string, goFiles []string) string {
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
		c := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", "^("+strings.Join(names, "|")+")$", pkgDir)
		c.Dir = root
		out, err := c.CombinedOutput()
		if err == nil {
			if strings.Contains(string(out), "no tests to run") {
				return fmt.Sprintf("new test file %s matched no tests — scoped run must fail, not report zero matches", rel)
			}
			return fmt.Sprintf("new test file %s passes — tests must fail before implementation (write a failing assertion first)", rel)
		}
		if strings.Contains(string(out), "build constraints exclude all Go files") || strings.Contains(string(out), "no Go files in") {
			return fmt.Sprintf("new test file %s is excluded by build constraints — tests must compile and fail, not be skipped", rel)
		}
	}
	return ""
}

// runGate reads the feature's required @s tags, runs the deterministic gate
// (coverage + green tests) via the same functions the CLI conductor uses, and
// returns the structured result. A missing feature file or unresolved test
// command is an error (the caller distinguishes "ran and failed" from "could
// not run").
func runGate(ctx context.Context, cfg config.Config, root, ticket, feature, coveredRaw, expect, newTestsRaw, gateBase string) (tddGateResult, error) {
	if feature == "" {
		return tddGateResult{}, fmt.Errorf("--feature is required")
	}
	// Validate expect: must be "red" or "green" (empty string defaults to "green")
	if expect == "" {
		expect = "green"
	}
	if expect != "red" && expect != "green" {
		return tddGateResult{}, fmt.Errorf("--expect must be red or green, got %q", expect)
	}

	// Resolve the per-feature base dir once: explicit --base wins (relative
	// paths join to root), else fall back to ticket/mtime resolution, else the
	// legacy flat layout.
	base := gateBase
	if base == "" {
		if b, ok := resolveTddBase(root, ticket); ok {
			base = b
		} else {
			base = filepath.Join(root, ".tu-agent", "tdd")
		}
	} else if !filepath.IsAbs(base) {
		base = filepath.Join(root, filepath.FromSlash(base))
	}

	if expect == "red" {
		runner, err := resolveTestRunner(cfg, root)
		if err != nil {
			return tddGateResult{}, err
		}
		since := time.Now()
		passed, _, rerr := runner(ctx)
		if rerr != nil {
			return tddGateResult{}, fmt.Errorf("test runner error: %w", rerr)
		}
		rep, lerr := testresult.LoadReports(root, since)
		if lerr != nil {
			return tddGateResult{}, fmt.Errorf("loading reports: %w", lerr)
		}
		newTests := splitTags(newTestsRaw)
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			var goFiles []string
			for _, rel := range newTests {
				if strings.HasSuffix(rel, ".go") {
					goFiles = append(goFiles, rel)
				}
			}
			if len(goFiles) > 0 {
				if fb := goNewTestsRed(ctx, root, goFiles); fb != "" {
					return tddGateResult{OK: false, Feedback: fb}, nil
				}
			}
		}
		res := evalRed(passed, rep, newTests)
		if res.OK {
			if err := writeRedBaseline(base, feature, root, newTests); err != nil {
				return tddGateResult{}, fmt.Errorf("runGate: %w", err)
			}
		}
		return res, nil
	}
	// expect green: read feature file from the per-feature dir
	featPath := filepath.Join(base, "features", feature+".feature")
	data, err := os.ReadFile(featPath)
	if err != nil {
		return tddGateResult{}, fmt.Errorf("reading feature: %w", err)
	}
	fb, warn := checkRedBaseline(base, feature, root)
	if fb != "" {
		return tddGateResult{OK: false, Feedback: fb}, nil
	}
	runner, err := resolveTestRunner(cfg, root)
	if err != nil {
		return tddGateResult{}, err
	}
	required := tdd.ScenarioTags(string(data))
	det := tdd.DeterministicJudge(ctx, runner, required, splitTags(coveredRaw))
	return tddGateResult{OK: det.OK, Feedback: det.Feedback, Warning: warn}, nil
}

var tddGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Run the deterministic gate (green tests + @s coverage) and print JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runGate(cmd.Context(), cfg, repoRoot(), tddGateTicket, tddGateFeature, tddGateCovered, tddGateExpect, tddGateNewTests, tddGateBase)
		if err != nil {
			return fmt.Errorf("tdd gate: %w", err)
		}
		out, err := json.Marshal(res)
		if err != nil {
			return fmt.Errorf("tdd gate: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	tddGateCmd.Flags().StringVar(&tddGateFeature, "feature", "", "feature name (reads <base>/features/<name>.feature)")
	tddGateCmd.Flags().StringVar(&tddGateCovered, "covered", "", "comma-separated @s tags the craftsman covered")
	tddGateCmd.Flags().StringVar(&tddGateExpect, "expect", "green", "expected color: green | red")
	tddGateCmd.Flags().StringVar(&tddGateNewTests, "new-tests", "", "comma-separated new test file paths (for --expect red)")
	tddGateCmd.Flags().StringVar(&tddGateTicket, "ticket", "", "ticket id to address a specific run's feature dir")
	tddGateCmd.Flags().StringVar(&tddGateBase, "base", "", "explicit per-feature base dir (overrides --ticket/mtime resolution)")
	tddCmd.AddCommand(tddGateCmd)
}
