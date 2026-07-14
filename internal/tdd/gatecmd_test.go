package tdd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/config"
	"github.com/carlosneir4/tu-agent/internal/testresult"
)

// testRunnerResolver builds the same command-backed TestRunner the cmd wrapper
// injects, driven by cfg.Tdd.TestCommand — a stand-in for the package-main
// resolveTestRunner so RunGate can be exercised without reaching into cmd/.
func testRunnerResolver(cfg config.Config, root string) (TestRunner, error) {
	cmdStr := cfg.Tdd.TestCommand
	return func(ctx context.Context) (bool, string, error) {
		c := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		c.Dir = root
		out, err := c.CombinedOutput()
		return err == nil, string(out), nil
	}, nil
}

func TestSplitTags(t *testing.T) {
	got := splitTags(" @s1, @s2 ,, @s3 ")
	want := []string{"@s1", "@s2", "@s3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	if splitTags("   ") != nil {
		t.Fatalf("blank must yield nil")
	}
}

// writeFileT writes rel (a repo-relative, slash-separated path) under root,
// creating parent directories as needed.
func writeFileT(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func writeFeature(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".tu-agent", "tdd", "features")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "@s1\nScenario: empty\n@s2\nScenario: many\n"
	if err := os.WriteFile(filepath.Join(dir, "count.feature"), []byte(body), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}
}

func TestRunGate(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)
	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	ctx := context.Background()

	// Seed a red baseline for this feature: GREEN now requires one (a missing
	// baseline is a gate failure, not a silent pass).
	if err := writeRedBaseline(filepath.Join(root, ".tu-agent", "tdd"), "count", root, nil); err != nil {
		t.Fatalf("writeRedBaseline: %v", err)
	}

	// All scenarios covered + tests green -> ok.
	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want ok, got %+v", res)
	}

	// A missing scenario -> not ok, feedback names it.
	res, err = RunGate(ctx, greenCfg, root, "", "count", "@s1", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK || !strings.Contains(res.Feedback, "@s2") {
		t.Fatalf("want not-ok naming @s2, got %+v", res)
	}

	// Covered but tests red -> not ok.
	res, err = RunGate(ctx, redCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on red tests, got %+v", res)
	}

	// Missing feature file -> error.
	if _, err := RunGate(ctx, greenCfg, root, "", "nope", "@s1", "green", "", "", testRunnerResolver); err == nil {
		t.Fatalf("want error for missing feature file")
	}
}

func TestRunGateExpectRed(t *testing.T) {
	// A runner that reports the suite red, with a report where the new test failed.
	rep := testresult.Report{Cases: []testresult.Case{
		{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail},
	}}
	res := evalRed(false, rep, []string{"src/test/java/com/acme/FooTest.java"})
	if !res.OK {
		t.Fatalf("expected red OK, got %+v", res)
	}
	// Green-on-arrival: suite red overall but the new test passed.
	rep2 := testresult.Report{Cases: []testresult.Case{
		{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass},
		{Class: "com.acme.OtherTest", Name: "y", Status: testresult.Fail},
	}}
	res2 := evalRed(false, rep2, []string{"src/test/java/com/acme/FooTest.java"})
	if res2.OK || !strings.Contains(res2.Feedback, "green without production") {
		t.Fatalf("expected green-on-arrival feedback, got %+v", res2)
	}
}

func TestRunGateInvalidExpect(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)
	cfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	// Invalid expect value "blue" should error, not silently run green path.
	_, err := RunGate(ctx, cfg, root, "", "count", "@s1", "blue", "", "", testRunnerResolver)
	if err == nil {
		t.Fatalf("want error for invalid expect value, got nil")
	}
	if !strings.Contains(err.Error(), "expect") {
		t.Fatalf("want error mentioning 'expect', got %v", err)
	}
}

func TestGateRedWritesBaseline(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)

	testDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	testFile := filepath.Join(testDir, "x_test.go")
	if err := os.WriteFile(testFile, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	ctx := context.Background()

	res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "pkg/x_test.go", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want red OK, got %+v", res)
	}

	base := filepath.Join(root, ".tu-agent", "tdd")
	data, err := os.ReadFile(baselinePath(base))
	if err != nil {
		t.Fatalf("reading baseline: %v", err)
	}
	var bl redBaseline
	if err := json.Unmarshal(data, &bl); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if bl.Feature != "count" {
		t.Fatalf("feature = %q, want count", bl.Feature)
	}
	want, err := sha256OfFile(testFile)
	if err != nil {
		t.Fatalf("hashing test file: %v", err)
	}
	if bl.Files["pkg/x_test.go"] != want {
		t.Fatalf("baseline hash = %q, want %q", bl.Files["pkg/x_test.go"], want)
	}
}

func TestGateGreenFailsOnMutatedTest(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)

	testDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	testFile := filepath.Join(testDir, "x_test.go")
	if err := os.WriteFile(testFile, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	base := filepath.Join(root, ".tu-agent", "tdd")
	if err := writeRedBaseline(base, "count", root, []string{"pkg/x_test.go"}); err != nil {
		t.Fatalf("writeRedBaseline: %v", err)
	}

	// Mutate the baselined test file after the baseline was recorded.
	if err := os.WriteFile(testFile, []byte("package pkg\n// weakened\n"), 0o644); err != nil {
		t.Fatalf("mutate test file: %v", err)
	}

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on mutated test file, got %+v", res)
	}
	if !strings.Contains(res.Feedback, "test files mutated since RED") {
		t.Fatalf("want feedback naming mutation, got %+v", res)
	}
}

func TestGateGreenFailsWithoutBaseline(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	// No baseline written for this feature -> the anti-cheat guard cannot be
	// verified, so the gate must fail rather than silently pass (skipping RED
	// entirely must not be a way to a green gate).
	res, err := RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok when the red baseline cannot be verified, got %+v", res)
	}
	if !strings.Contains(res.Feedback, "red baseline") {
		t.Fatalf("want feedback naming the missing/unusable baseline, got %+v", res)
	}
}

// TestGateRedGoScopedProof proves the Go RED path scoped-per-file guard: a
// "cheating" new test file that already passes must be rejected even though
// no suite-level failure exists to catch it, and a genuinely red test file
// must be accepted.
func TestGateRedGoScopedProof(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	writeFileT(t, root, "pkg/pkg.go", "package pkg\n\nfunc Val() int { return 1 }\n")
	// A "cheating" new test that already PASSES: scoped proof must reject it.
	writeFileT(t, root, "pkg/pkg_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestVal(t *testing.T) {\n\tif Val() != 1 {\n\t\tt.Fatal(\"nope\")\n\t}\n}\n")
	fb := goNewTestsRed(context.Background(), root, []string{"pkg/pkg_test.go"})
	if fb == "" || !strings.Contains(fb, "passes") {
		t.Fatalf("passing new test not rejected: %q", fb)
	}
	// A genuinely red test: scoped proof accepts it.
	writeFileT(t, root, "pkg/red_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) {\n\tif Val() != 2 {\n\t\tt.Fatal(\"red\")\n\t}\n}\n")
	if fb := goNewTestsRed(context.Background(), root, []string{"pkg/red_test.go"}); fb != "" {
		t.Fatalf("red test rejected: %q", fb)
	}
	// A new test file that fails to COMPILE (undefined symbol) is not a
	// legitimately-failing test — it must be rejected with build-failure
	// feedback, not accepted as red.
	writeFileT(t, root, "pkg/compileerr_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestCompileErr(t *testing.T) {\n\tundefinedFunc()\n}\n")
	fb = goNewTestsRed(context.Background(), root, []string{"pkg/compileerr_test.go"})
	if fb == "" || !strings.Contains(fb, "build") {
		t.Fatalf("build failure accepted as legitimate red: %q", fb)
	}
}

// TestGateRedGoScopedBuildTagExcluded proves a one-line cheat: a new test file
// that is the ONLY file in a brand-new package and carries an undefined build
// tag makes the scoped `go test -run ... ./dir` exit non-zero with "build
// constraints exclude all Go files" — the file was never compiled, let alone
// run, so it must NOT be accepted as red (it could never turn green later).
func TestGateRedGoScopedBuildTagExcluded(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	writeFileT(t, root, "pkg2/excluded_test.go",
		"//go:build neverbuild\n\npackage pkg2\n\nimport \"testing\"\n\nfunc TestExcluded(t *testing.T) {}\n")
	fb := goNewTestsRed(context.Background(), root, []string{"pkg2/excluded_test.go"})
	if fb == "" {
		t.Fatalf("build-tag-excluded new test file accepted as red — permanent cheat: file can never turn green")
	}
	if !strings.Contains(fb, "excluded by build constraints") {
		t.Fatalf("want feedback naming the build-constraint exclusion, got %q", fb)
	}
}

// TestGateRedGoScopedNoTestsToRun proves the belt-and-braces guard: a `-run`
// that matches no tests in a mixed package (Test funcs excluded per-function
// via build tags, so the file-level regex still finds a Test declaration)
// must not be accepted as red — the scoped run exits 0 with "no tests to run"
// rather than actually executing and failing the new test.
func TestGateRedGoScopedNoTestsToRun(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	// A sibling _test.go file (no build tag) keeps the package's test binary
	// buildable, but the new test file's own Test func is excluded by its
	// build tag, so -run for that exact name matches nothing in the package
	// (a mixed package: the file-level regex still finds "TestNew" declared
	// in source, but the compiled binary never runs it).
	writeFileT(t, root, "pkg3/other_test.go", "package pkg3\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n")
	writeFileT(t, root, "pkg3/new_test.go",
		"//go:build neverbuild\n\npackage pkg3\n\nimport \"testing\"\n\nfunc TestNew(t *testing.T) {}\n")
	fb := goNewTestsRed(context.Background(), root, []string{"pkg3/new_test.go"})
	if fb == "" {
		t.Fatalf("new test file whose -run matched nothing was accepted as red")
	}
}

// TestGateRedGoScopedWiring exercises the real RunGate entry point (not just
// goNewTestsRed directly, which is all the other tests in this file cover) to
// prove the scoped Go RED check is actually reachable from the gate command:
// a passing new test file must reject through RunGate with feedback naming
// the file and must NOT write a red baseline, and a build-tag-excluded new
// test file must reject through RunGate with the build-constraints feedback.
func TestGateRedGoScopedWiring(t *testing.T) {
	t.Run("passing new test rejected, no baseline written", func(t *testing.T) {
		root := t.TempDir()
		writeFeature(t, root)
		writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
		writeFileT(t, root, "pkg/pkg.go", "package pkg\n\nfunc Val() int { return 1 }\n")
		// A "cheating" new test that already PASSES.
		writeFileT(t, root, "pkg/pkg_test.go",
			"package pkg\n\nimport \"testing\"\n\nfunc TestVal(t *testing.T) {\n\tif Val() != 1 {\n\t\tt.Fatal(\"nope\")\n\t}\n}\n")

		redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
		ctx := context.Background()

		res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "pkg/pkg_test.go", "", testRunnerResolver)
		if err != nil {
			t.Fatalf("runGate: %v", err)
		}
		if res.OK {
			t.Fatalf("want not-ok through runGate for a passing new test file, got %+v", res)
		}
		if !strings.Contains(res.Feedback, "pkg/pkg_test.go") {
			t.Fatalf("want feedback naming the file, got %+v", res)
		}

		base := filepath.Join(root, ".tu-agent", "tdd")
		if _, err := os.Stat(baselinePath(base)); !os.IsNotExist(err) {
			t.Fatalf("want no red baseline written for a rejected proof, stat err=%v", err)
		}
	})

	t.Run("build-tag-excluded new test rejected through runGate", func(t *testing.T) {
		root := t.TempDir()
		writeFeature(t, root)
		writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
		writeFileT(t, root, "pkg2/excluded_test.go",
			"//go:build neverbuild\n\npackage pkg2\n\nimport \"testing\"\n\nfunc TestExcluded(t *testing.T) {}\n")

		redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
		ctx := context.Background()

		res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "pkg2/excluded_test.go", "", testRunnerResolver)
		if err != nil {
			t.Fatalf("runGate: %v", err)
		}
		if res.OK {
			t.Fatalf("want not-ok through runGate for a build-tag-excluded new test file, got %+v", res)
		}
		if !strings.Contains(res.Feedback, "excluded by build constraints") {
			t.Fatalf("want feedback naming the build-constraint exclusion, got %+v", res)
		}

		base := filepath.Join(root, ".tu-agent", "tdd")
		if _, err := os.Stat(baselinePath(base)); !os.IsNotExist(err) {
			t.Fatalf("want no red baseline written for a rejected proof, stat err=%v", err)
		}
	})
}

// TestGateRedGoScopedNotOverriddenBySuiteVerdict guards against
// bug-pattern/red-gate-suite-scope-override: when goNewTestsRed confirms every
// new Go test file is genuinely red on its own, that per-file proof must be
// the verdict even if the project's overall test command reports green (e.g.
// because it does not yet cover the new package) and there is no JUnit report
// to contradict it. evalRed must not be allowed to overturn a confirmed
// per-file red result.
func TestGateRedGoScopedNotOverriddenBySuiteVerdict(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	writeFileT(t, root, "pkg/pkg.go", "package pkg\n\nfunc Val() int { return 1 }\n")
	writeFileT(t, root, "pkg/red_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) {\n\tif Val() != 2 {\n\t\tt.Fatal(\"red\")\n\t}\n}\n")

	// The project's overall test command reports green (does not cover this
	// package), but the scoped per-file proof genuinely fails.
	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	ctx := context.Background()

	res, err := RunGate(ctx, greenCfg, root, "", "count", "", "red", "pkg/red_test.go", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("scoped per-file red proof overridden by green suite verdict, got %+v", res)
	}

	base := filepath.Join(root, ".tu-agent", "tdd")
	if _, err := os.Stat(baselinePath(base)); err != nil {
		t.Fatalf("want red baseline written, stat err=%v", err)
	}
}

// TestGateRedGoScopedDoesNotBypassNonGoRedProof guards against a mixed-
// language RED gap: when a feature has BOTH a genuinely-red new Go test file
// AND a non-Go (e.g. JUnit) new test file, a confirmed Go per-file red proof
// must NOT bypass evalRed for the non-Go file — otherwise a JUnit test that
// is green-on-arrival (never proven red) would be silently baselined just
// because a sibling Go test happened to be red.
func TestGateRedGoScopedDoesNotBypassNonGoRedProof(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root)
	writeFileT(t, root, "go.mod", "module fixture\n\ngo 1.22\n")
	writeFileT(t, root, "pkg/pkg.go", "package pkg\n\nfunc Val() int { return 1 }\n")
	// A genuinely red Go new test — the scoped per-file proof accepts it.
	writeFileT(t, root, "pkg/red_test.go",
		"package pkg\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) {\n\tif Val() != 2 {\n\t\tt.Fatal(\"red\")\n\t}\n}\n")
	// A sibling non-Go (JUnit) new test that is GREEN-ON-ARRIVAL — a cheat
	// evalRed must still catch via the JUnit report.
	writeFileT(t, root, "src/test/java/com/acme/FooTest.java",
		"package com.acme;\nclass FooTest {}\n")
	writeFileT(t, root, "target/surefire-reports/TEST-com.acme.FooTest.xml",
		`<testsuite><testcase classname="com.acme.FooTest" name="testFoo"/></testsuite>`)

	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	ctx := context.Background()

	newTests := "pkg/red_test.go,src/test/java/com/acme/FooTest.java"
	res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", newTests, "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("non-Go new test green-on-arrival must not be baselined just because a sibling Go test is red, got %+v", res)
	}
	if !strings.Contains(res.Feedback, "FooTest.java") {
		t.Fatalf("want feedback naming the green JUnit test, got %+v", res)
	}

	base := filepath.Join(root, ".tu-agent", "tdd")
	if _, err := os.Stat(baselinePath(base)); !os.IsNotExist(err) {
		t.Fatalf("want no red baseline written when the non-Go new test was not proven red, stat err=%v", err)
	}
}

// TestGateGreenSameBaseAsRedRegardlessOfMtime guards against the gate
// resolving a different base dir for GREEN than the one RED wrote the
// baseline to, when an unrelated run dir with a newer mtime exists for the
// same repo/ticket. The feature's run dir is identified by containing
// features/<feature>.feature (written once at spec time, stable across the
// RED/GREEN window) rather than by mtime.
func TestGateGreenSameBaseAsRedRegardlessOfMtime(t *testing.T) {
	root := t.TempDir()

	featureDir := filepath.Join(root, ".tu-agent", "tdd", "feat-a-count", "features")
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "@s1\nScenario: empty\n@s2\nScenario: many\n"
	if err := os.WriteFile(filepath.Join(featureDir, "count.feature"), []byte(body), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}

	testFile := filepath.Join(root, "pkg", "x_test.go")
	writeFileT(t, root, "pkg/x_test.go", "package pkg\n")

	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	ctx := context.Background()

	// RED (base=""): only feat-a-count exists, so it resolves there and
	// writes the baseline into it.
	res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "pkg/x_test.go", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate red: %v", err)
	}
	if !res.OK {
		t.Fatalf("want red OK, got %+v", res)
	}
	redBase := filepath.Join(root, ".tu-agent", "tdd", "feat-a-count")
	if _, err := os.Stat(baselinePath(redBase)); err != nil {
		t.Fatalf("baseline not written to feature's dir: %v", err)
	}

	// A newer, unrelated run dir for a different feature appears between RED
	// and GREEN — it must NOT be picked over the feature's own dir.
	otherDir := filepath.Join(root, ".tu-agent", "tdd", "feat-b-other", "features")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "other.feature"), []byte("@s1\nScenario: x\n"), 0o644); err != nil {
		t.Fatalf("write other feature: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Dir(otherDir), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Mutate the baselined test file after RED, before GREEN.
	if err := os.WriteFile(testFile, []byte("package pkg\n// weakened\n"), 0o644); err != nil {
		t.Fatalf("mutate test file: %v", err)
	}

	greenCfg := config.Config{Tdd: config.TddConfig{TestCommand: "true"}}

	// GREEN (base=""): must still resolve to feat-a-count (has the feature
	// file) and enforce the anti-cheat guard, proving it did not resolve to
	// the newer, unrelated feat-b-other dir.
	res, err = RunGate(ctx, greenCfg, root, "", "count", "@s1,@s2", "green", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("runGate green: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on mutated test file, got %+v", res)
	}
	if !strings.Contains(res.Feedback, "test files mutated since RED") {
		t.Fatalf("want feedback naming mutation (proves it read the feature's own baseline), got %+v", res)
	}
}
