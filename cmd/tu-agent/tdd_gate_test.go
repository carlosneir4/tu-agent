package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/testresult"
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
	res, err := runGate(ctx, greenCfg, root, "count", "@s1,@s2", "green", "")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want ok, got %+v", res)
	}

	// A missing scenario -> not ok, feedback names it.
	res, err = runGate(ctx, greenCfg, root, "count", "@s1", "green", "")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK || !strings.Contains(res.Feedback, "@s2") {
		t.Fatalf("want not-ok naming @s2, got %+v", res)
	}

	// Covered but tests red -> not ok.
	res, err = runGate(ctx, redCfg, root, "count", "@s1,@s2", "green", "")
	if err != nil {
		t.Fatalf("runGate: %v", err)
	}
	if res.OK {
		t.Fatalf("want not-ok on red tests, got %+v", res)
	}

	// Missing feature file -> error.
	if _, err := runGate(ctx, greenCfg, root, "nope", "@s1", "green", ""); err == nil {
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
	_, err := runGate(ctx, cfg, root, "count", "@s1", "blue", "")
	if err == nil {
		t.Fatalf("want error for invalid expect value, got nil")
	}
	if !strings.Contains(err.Error(), "expect") {
		t.Fatalf("want error mentioning 'expect', got %v", err)
	}
}
