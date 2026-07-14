package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// findRootStatusCmd returns the top-level `status` command registered directly
// on rootCmd, or nil if none exists. rootCmd.Commands() only returns direct
// children, so the existing `learn status`, `graph status`, and `tdd status`
// subcommands (grandchildren) are correctly excluded — only a genuine
// root-level `status` matches. Today no such command exists, so this returns
// nil and every scenario below goes red without a compile break.
func findRootStatusCmd() *cobra.Command {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "status" {
			return c
		}
	}
	return nil
}

// runTopLevelStatus locates the root-level status command and runs it, capturing
// stdout. If the command is missing (current state) or has no RunE, it returns a
// non-nil error and empty output — which is what drives the RED assertions.
func runTopLevelStatus(t *testing.T) (string, error) {
	t.Helper()
	cmd := findRootStatusCmd()
	return captureStdout(t, func() error {
		if cmd == nil {
			return fmt.Errorf("no top-level `status` command is registered on rootCmd")
		}
		if cmd.RunE == nil {
			return fmt.Errorf("top-level `status` command must define RunE")
		}
		cmd.SetContext(context.Background())
		return cmd.RunE(cmd, nil)
	})
}

// @s1 — the status command is registered on the root command as read-only.
func TestTopLevelStatus_RegisteredReadOnly(t *testing.T) {
	cmd := findRootStatusCmd()
	if cmd == nil {
		t.Fatal("no direct subcommand of rootCmd with Use \"status\" is registered")
	}
	if cmd.Use != "status" {
		t.Errorf("cmd.Use = %q, want \"status\"", cmd.Use)
	}
	// cobra.NoArgs must reject any positional argument. Args being nil (the
	// default) would accept arbitrary args, which this scenario forbids.
	if cmd.Args == nil {
		t.Fatal("status command must set Args to cobra.NoArgs (Args is nil, which accepts arbitrary args)")
	}
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("status command Args must reject positional arguments (cobra.NoArgs), got nil error for [\"x\"]")
	}
}

// @s2 — output carries both a Graph section and a Knowledge section, and the
// Graph section reproduces runGraphStatus() verbatim.
func TestTopLevelStatus_ComposesGraphAndKnowledge(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")
	t.Chdir(root)

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	seedConcept(t) // at least one concept so the knowledge half also succeeds

	out, err := runTopLevelStatus(t)
	if err != nil {
		t.Fatalf("status command must succeed when both halves render: %v", err)
	}
	if !strings.Contains(out, "Graph") {
		t.Errorf("output missing a Graph section header:\n%s", out)
	}
	if !strings.Contains(out, "Knowledge") {
		t.Errorf("output missing a Knowledge (learn) section header:\n%s", out)
	}
	// The Graph section must reproduce exactly what runGraphStatus() returns.
	// It is read-only and deterministic against the same store, so the string is
	// identical whether computed here or inside the command.
	gs, gsErr := runGraphStatus()
	if gsErr != nil {
		t.Fatalf("runGraphStatus (test baseline): %v", gsErr)
	}
	if strings.TrimSpace(gs) == "" {
		t.Fatal("test setup: runGraphStatus returned empty; graph must be populated")
	}
	if !strings.Contains(out, gs) {
		t.Errorf("Graph section does not reproduce runGraphStatus() output.\nwant substring:\n%q\ngot:\n%s", gs, out)
	}
}

// @s3 — one failing half (no concepts) is reported inline; the other half still
// renders and the command exits 0.
func TestTopLevelStatus_NoConceptsKnowledgeErrorInline(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")
	t.Chdir(root)

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	// Deliberately do NOT seed a concept: the graph store is populated but the
	// knowledge index has zero concepts, so runStatus errors "no concepts found".

	out, err := runTopLevelStatus(t)
	if err != nil {
		t.Fatalf("status must exit 0 (RunE returns nil) even when the knowledge half errors, got: %v", err)
	}
	if !strings.Contains(out, "Graph") {
		t.Errorf("output missing a Graph section header:\n%s", out)
	}
	// The Graph half succeeded, so its content must be present.
	gs, gsErr := runGraphStatus()
	if gsErr != nil {
		t.Fatalf("runGraphStatus (test baseline): %v", gsErr)
	}
	if !strings.Contains(out, gs) {
		t.Errorf("Graph section does not show the graph status content.\nwant substring:\n%q\ngot:\n%s", gs, out)
	}
	if !strings.Contains(out, "Knowledge") {
		t.Errorf("output missing a Knowledge (learn) section header:\n%s", out)
	}
	// The knowledge half's error text must be surfaced inline in its section.
	// runStatus returns (does not print) this error, so the command must render it.
	if !strings.Contains(out, "no concepts found") {
		t.Errorf("Knowledge section must show the \"no concepts\" error text inline:\n%s", out)
	}
}

// @s4 — a failing graph half (store fails to open) does not suppress the
// knowledge half; the command still exits 0 and both sections render.
//
// NOTE (documented per contract): the knowledge half loads concepts from the
// SAME graph store (loadConceptSkills -> openGraphStore), so with the current
// architecture a genuine "store fails to open" cannot coexist with a readable
// concept index — both halves observe the open failure. The feature invariant
// that is actually testable here is: the graph failure is reported inline in the
// Graph section AND does not abort the command or suppress the Knowledge
// section. That is what this test pins.
func TestTopLevelStatus_GraphOpenFailureDoesNotSuppressKnowledge(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")
	t.Chdir(root)

	// Force openGraphStore to fail deterministically: create the graph.db path
	// as a NON-EMPTY directory. store.Open self-heals an unreadable db by
	// deleting and retrying once, but os.Remove of a non-empty directory fails,
	// so Open gives up and returns an error (an empty dir would be removed and
	// silently rebuilt, defeating the failure injection).
	dbDir := filepath.Join(root, ".tu-agent", "graph.db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Confirm the injection is effective before exercising the command.
	if _, gsErr := runGraphStatus(); gsErr == nil {
		t.Fatal("test setup: expected runGraphStatus to fail with the graph store unopenable")
	}

	out, err := runTopLevelStatus(t)
	if err != nil {
		t.Fatalf("status must exit 0 (RunE returns nil) even when the graph half fails to open, got: %v", err)
	}

	gHdr := strings.Index(out, "Graph")
	kHdr := strings.Index(out, "Knowledge")
	if gHdr < 0 {
		t.Errorf("output missing a Graph section header:\n%s", out)
	}
	if kHdr < 0 {
		t.Errorf("output missing a Knowledge (learn) section header:\n%s", out)
	}
	if gHdr >= 0 && kHdr >= 0 && gHdr >= kHdr {
		t.Errorf("Graph section must precede the Knowledge section (graph at %d, knowledge at %d):\n%s", gHdr, kHdr, out)
	}
	// The Graph section must show the open error as its content.
	if _, gsErr := runGraphStatus(); gsErr != nil {
		if !strings.Contains(out, gsErr.Error()) {
			t.Errorf("Graph section must show the open-error text %q inline:\n%s", gsErr.Error(), out)
		}
	}
	// The Knowledge section must still be rendered with its own content — the
	// graph failure must not truncate the output at the Graph section.
	if kHdr >= 0 {
		body := strings.TrimSpace(out[kHdr+len("Knowledge"):])
		if body == "" {
			t.Errorf("Knowledge section is empty; the graph failure must not suppress it:\n%s", out)
		}
	}
}
