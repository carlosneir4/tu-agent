package main

import (
	"context"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
)

// RED-phase test for feature `mem-reconcile-mcp` (leg 5, feature 4 of 4) —
// scenario @s3 of
//
//	.tu-agent/tdd/crystallize-community-detection-clusteri/features/mem-reconcile-mcp.feature
//
// §10 dual-availability: `mem_reconcile` must be registered in the bundled MCP
// server tool registry so the plugin inherits it, listed by `tu-agent mcp
// --list` alongside the sibling mutators mem_rescope and mem_delete.
//
// This mirrors the existing registry tests (TestMCPToolNamesIncludesMutators,
// TestMemRelationToolsRegistered): it asserts the tool is actually served by
// newMCPServer (enumerated via the MCP client), so the failure is an ASSERTION
// failure (mem_reconcile absent), NOT a compile break of package main. It goes
// GREEN once newMCPServer registers the tool.
func TestMemReconcileToolRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	names := servedToolNames(t)

	// Anchor: the sibling mutators must already be present. If they are not, the
	// registry itself changed shape and the @s3 assertion below is meaningless.
	for _, anchor := range []string{"mem_rescope", "mem_delete"} {
		if !names[anchor] {
			t.Fatalf("precondition: newMCPServer does not serve sibling mutator %q", anchor)
		}
	}

	if !names["mem_reconcile"] {
		t.Errorf("newMCPServer does not serve %q; the bundled MCP server must register mem_reconcile alongside mem_rescope and mem_delete (§10 dual-availability)", "mem_reconcile")
	}
}

// -----------------------------------------------------------------------------
// B2 apply-path additions (feature `reconcile-apply-wiring`). These reference
// memReconcileMCPInput.{Apply,Topic,ToCluster,Name}, which do NOT exist yet, so
// this file fails to COMPILE — the correct RED, scoped to cmd/tu-agent. Helpers
// (setReconcileFlag, resetReconcileState, runReconcileApply, reconcileRecord,
// isOrphanTopic) live in reconcile_apply_test.go (same package). The MCP tool
// and CLI command drive ONE shared adapter, so their apply output text is
// byte-identical (§10 parity, asserted in @test-9).
// -----------------------------------------------------------------------------

// @test-7 — `mem_reconcile` with apply=true mutates (rebinds the selected orphan
// to the forced cluster) and returns the RenderApplyResult text.
func TestHandleMemReconcile_ApplyRebinds(t *testing.T) {
	t.Chdir(t.TempDir())
	seedCluster(t) // live "checkout" cluster
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	_, out, err := handleMemReconcile(context.Background(), nil,
		memReconcileMCPInput{Min: 3, Apply: true, Topic: "skill/acme-orphan-a", ToCluster: "checkout"})
	if err != nil {
		t.Fatalf("mem_reconcile apply=true: %v", err)
	}
	if !strings.Contains(out.Result, "Applied reconcile") || !strings.Contains(out.Result, "skill/acme-orphan-a") {
		t.Errorf("apply result missing the rebound line:\n%s", out.Result)
	}
	if isOrphanTopic(t, "skill/acme-orphan-a", 3) {
		t.Errorf("skill/acme-orphan-a still an orphan after mem_reconcile apply=true")
	}
	rec, _ := reconcileRecord(t, "skill/acme-orphan-a")
	if got := crystallize.ParseLabel(rec.Content); got != "checkout" {
		t.Errorf("rebound label = %q, want %q", got, "checkout")
	}
}

// @test-8 — `mem_reconcile` with apply omitted defaults to dry-run: it reports
// the plan and mutates nothing.
func TestHandleMemReconcile_DefaultsToDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	seedCluster(t)
	seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")

	before, _ := reconcileRecord(t, "skill/acme-orphan-a")

	_, out, err := handleMemReconcile(context.Background(), nil,
		memReconcileMCPInput{Min: 3}) // Apply omitted
	if err != nil {
		t.Fatalf("mem_reconcile dry-run: %v", err)
	}
	if !strings.Contains(out.Result, "Reconcile plan") {
		t.Errorf("dry-run did not report a plan:\n%s", out.Result)
	}
	if strings.Contains(out.Result, "Applied reconcile") {
		t.Errorf("dry-run applied changes:\n%s", out.Result)
	}
	after, ok := reconcileRecord(t, "skill/acme-orphan-a")
	if !ok {
		t.Fatalf("skill/acme-orphan-a vanished under mem_reconcile dry-run")
	}
	if after.Content != before.Content || after.Revision != before.Revision {
		t.Errorf("dry-run mutated the record:\n before=%+v\n after=%+v", before, after)
	}
}

// @test-9 — §10 parity: for the SAME fixed corpus, the CLI `--apply` output text
// equals the MCP apply output text (both funnel through the shared adapter).
// Because apply mutates, each surface runs against its own identical, freshly
// seeded store/temp dir, so both see the same starting state.
func TestReconcileApply_CLIvsMCPParity(t *testing.T) {
	seed := func(t *testing.T) {
		t.Helper()
		seedCluster(t) // live "checkout" cluster
		seedOrphanSkill(t, "acme-orphan-a", "acme-ghost-a")
	}

	var cliOut, mcpOut string

	t.Run("cli", func(t *testing.T) {
		t.Chdir(t.TempDir())
		resetReconcileState(t)
		memReconcileMin = 3
		seed(t)
		setReconcileFlag(t, "apply", "true")
		setReconcileFlag(t, "topic", "skill/acme-orphan-a")
		setReconcileFlag(t, "to-cluster", "checkout")
		out, err := runReconcileApply(t)
		if err != nil {
			t.Fatalf("CLI apply: %v", err)
		}
		cliOut = out
	})

	t.Run("mcp", func(t *testing.T) {
		t.Chdir(t.TempDir())
		seed(t)
		_, out, err := handleMemReconcile(context.Background(), nil,
			memReconcileMCPInput{Min: 3, Apply: true, Topic: "skill/acme-orphan-a", ToCluster: "checkout"})
		if err != nil {
			t.Fatalf("MCP apply: %v", err)
		}
		mcpOut = out.Result
	})

	if cliOut == "" || mcpOut == "" {
		t.Fatalf("empty apply output: cli=%q mcp=%q", cliOut, mcpOut)
	}
	if cliOut != mcpOut {
		t.Errorf("§10 parity mismatch between CLI and MCP apply output:\nCLI=%q\nMCP=%q", cliOut, mcpOut)
	}
	if !strings.Contains(cliOut, "Applied reconcile") {
		t.Errorf("parity output is not an apply result:\n%s", cliOut)
	}
}
