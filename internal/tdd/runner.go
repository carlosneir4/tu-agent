package tdd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// ResolveTestRunner builds the deterministic gate's TestRunner. Resolution:
//  1. cfg.Tdd.TestCommand set    -> run it via `sh -c`.
//  2. empty + go.mod at repoRoot -> default `go test ./...`.
//  3. empty + no go.mod          -> error (caller must fail fast).
func ResolveTestRunner(cfg config.Config, repoRoot string) (TestRunner, error) {
	if cmd := strings.TrimSpace(cfg.Tdd.TestCommand); cmd != "" {
		return func(ctx context.Context) (bool, string, error) {
			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Dir = repoRoot
			return runTestCmd(c)
		}, nil
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
		return func(ctx context.Context) (bool, string, error) {
			c := exec.CommandContext(ctx, "go", "test", "./...")
			c.Dir = repoRoot
			return runTestCmd(c)
		}, nil
	}
	return nil, fmt.Errorf("no test command configured; set tdd.test_command in .tu-agent/config.yaml")
}

// runTestCmd runs cmd and maps the outcome onto the TestRunner contract:
// an *exec.ExitError means the tests ran and failed; any other error means
// the command could not run.
func runTestCmd(cmd *exec.Cmd) (bool, string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, string(out), nil // tests ran and failed
		}
		return false, string(out), err // could not run
	}
	return true, string(out), nil
}

// RunVerify runs the resolved test command and reports whether it passed. It
// returns an error only when the runner itself could not run (misconfigured
// or missing test command) — a failing test suite is a normal (false, nil)
// result, not an error, so `tdd verify` can print {"ok":false} with exit 0.
func RunVerify(ctx context.Context, cfg config.Config, root string) (bool, error) {
	runner, err := ResolveTestRunner(cfg, root)
	if err != nil {
		return false, fmt.Errorf("runVerify: %w", err)
	}
	passed, _, err := runner(ctx)
	if err != nil {
		return false, fmt.Errorf("runVerify: %w", err)
	}
	return passed, nil
}
