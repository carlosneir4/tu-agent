package main

import (
	"os"
	"strings"
	"testing"
)

func TestSessionCLILifecycle(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runSessionStart("", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := runSessionEnd("", "did the thing", nil); err != nil {
		t.Fatalf("end: %v", err)
	}
	var out strings.Builder
	if err := runSessionStart("", &out); err != nil {
		t.Fatalf("start 2: %v", err)
	}
	if !strings.Contains(out.String(), "did the thing") {
		t.Errorf("start should print previous summary, got %q", out.String())
	}
}
