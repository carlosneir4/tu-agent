package testgen

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerSuccess(t *testing.T) {
	out, err := ExecRunner(context.Background(), t.TempDir(), []string{"echo", "hello"}, 5*time.Second)
	if err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestExecRunnerFailure(t *testing.T) {
	_, err := ExecRunner(context.Background(), t.TempDir(), []string{"false"}, 5*time.Second)
	if err == nil {
		t.Fatal("want error from failing command")
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	_, err := ExecRunner(context.Background(), t.TempDir(), []string{"sleep", "5"}, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
}
