package testgen

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// ExecRunner is the default Runner: it shells out to the adapter's scoped
// test command. The timeout counts as a failed attempt upstream (spec).
func ExecRunner(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return string(out), fmt.Errorf("test run timed out after %s", timeout)
	}
	if err != nil {
		return string(out), fmt.Errorf("testgen.ExecRunner: %w", err)
	}
	return string(out), nil
}
