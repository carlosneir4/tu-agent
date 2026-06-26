package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/config"
)

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

	// All scenarios covered + tests green -> ok.
	res, err := runGate(ctx, greenCfg, root, "count", "@s1,@s2")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want ok, got %+v", res)
	}

	// A missing scenario -> not ok, feedback names it.
	res, err = runGate(ctx, greenCfg, root, "count", "@s1")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK || !strings.Contains(res.Feedback, "@s2") {
		t.Fatalf("want not-ok naming @s2, got %+v", res)
	}

	// Covered but tests red -> not ok.
	res, err = runGate(ctx, redCfg, root, "count", "@s1,@s2")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on red tests, got %+v", res)
	}

	// Missing feature file -> error.
	if _, err := runGate(ctx, greenCfg, root, "nope", "@s1"); err == nil {
		t.Fatalf("want error for missing feature file")
	}
}
