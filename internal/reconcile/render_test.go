package reconcile

// RED-phase tests for feature `mem-reconcile-mcp` (leg 5, feature 4 of 4) —
// scenarios @s1 and @s2 of
//   .tu-agent/tdd/crystallize-community-detection-clusteri/features/mem-reconcile-mcp.feature
//
// §10 dual-availability: reconciliation ships as a CLI subcommand
// `memory reconcile` AND an MCP tool `mem_reconcile`. Both surfaces drive ONE
// shared core so they emit the identical plan text. Feature 4 adds the missing
// piece: a deterministic PLAN-TEXT RENDERER over reconcile.Plan that both
// surfaces call. Making the renderer a single pure function is what makes @s1
// parity STRUCTURAL (same function → same text) rather than a fragile diff of
// two independently-written renderers.
//
// Production API this file expects (to be ADDED to internal/reconcile — until it
// exists this file fails to COMPILE, the correct RED, ISOLATED to
// internal/reconcile: the plugin/cmd registry test for @s3 lives in package main
// and compiles against the existing registry, so it fails as an ASSERTION, not a
// compile break):
//
//   // RenderPlan renders a dry-run reconcile Plan to deterministic, byte-stable
//   // plan text. Pure: identical Plans render to identical text. It is the single
//   // renderer both `memory reconcile` (CLI) and `mem_reconcile` (MCP) call, so
//   // the two surfaces are byte-identical by construction. For each orphan the
//   // text names at least the record's topic key and its bound (parsed) label.
//   func RenderPlan(p Plan) string
//
// Fixtures are generic and fictional (acme-*) per repo §9. Helpers orphanRecord,
// mem, snapshotStore and snapshotFiles are shared with plan_test.go (same
// package).

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// seedReconcileFixture builds a fixed corpus — one cohesive live "checkout"
// cluster plus one orphan skill record whose bound label matches no live
// cluster — in a real store, and materializes the orphan's (marked) skill folder
// on disk. It returns the open store, the skills dir, and the orphan record so
// callers can assert against its topic/label. The corpus is deliberately the
// same shape both surfaces would see.
func seedReconcileFixture(t *testing.T) (*memory.Store, string, memory.Observation) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := store.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	orphan := orphanRecord("acme-orphan", "acme-ghost", []memory.Observation{mem("reference/acme-ghost")})
	if _, err := store.Upsert(orphan.TopicKey, orphan.Content, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}

	skillsDir := t.TempDir()
	ghostSkill := filepath.Join(skillsDir, "acme-ghost", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(ghostSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghostSkill, []byte(orphan.Content), 0o644); err != nil {
		t.Fatal(err)
	}
	return store, skillsDir, orphan
}

// @s1 — the MCP tool and the CLI dry-run emit the IDENTICAL plan text for the
// same fixed corpus. Both surfaces route the plan through the single RenderPlan
// core, so parity is structural: this test proves the renderer is a pure
// function of the Plan by rendering the SAME corpus built two ways — via the
// store adapter DryRun (the path the MCP handler runs) and via the pure planner
// PlanFrom over the store's own observations (the CLI path) — and asserting the
// two texts are byte-identical. The plan text is also non-vacuous: it names the
// orphan's topic key and its bound label, so "identical" is not "both empty".
func TestRenderPlan_SurfaceParity_SharedCore(t *testing.T) {
	store, skillsDir, orphan := seedReconcileFixture(t)

	// MCP path: read the live store + scan the skills dir, then render.
	mcpPlan, err := DryRun(store, skillsDir, 3)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	mcpText := RenderPlan(mcpPlan)

	// CLI path: render a plan built by the pure planner over the store's own
	// observations. DryRun is PlanFrom(store.List(), scanFolders(...)), so the
	// two routes must produce byte-identical text — that is the §10 parity
	// guarantee both surfaces inherit from the shared renderer.
	obs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	cliText := RenderPlan(PlanFrom(obs, nil, 3))

	if mcpText != cliText {
		t.Errorf("mem_reconcile and `memory reconcile` plan text differ (§10 parity broken):\nMCP:\n%s\nCLI:\n%s", mcpText, cliText)
	}

	// Non-vacuous: the plan actually reports the orphan by topic and bound label.
	if len(mcpPlan.Orphans) == 0 {
		t.Fatalf("fixture produced no orphans; expected %q to be one", orphan.TopicKey)
	}
	for _, want := range []string{orphan.TopicKey, "acme-ghost"} {
		if !strings.Contains(mcpText, want) {
			t.Errorf("plan text missing %q (a reconcile plan must name the orphan's topic and bound label):\n%s", want, mcpText)
		}
	}
}

// @s2 — mem_reconcile defaults to dry-run: the default action (no apply flag)
// renders the plan and mutates NO observation rows and NO skill files. This
// exercises the exact shared path the tool runs by default —
// RenderPlan(DryRun(...)) — and asserts every observation row + sync_id and
// every skills/*/SKILL.md byte is identical before and after, while the plan
// text is still produced (so "reports the plan" is not vacuous).
func TestRenderPlan_DefaultDryRun_MutatesNothing(t *testing.T) {
	store, skillsDir, orphan := seedReconcileFixture(t)

	before := snapshotStore(t, store)
	beforeFiles := snapshotFiles(t, skillsDir)

	plan, err := DryRun(store, skillsDir, 3)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	text := RenderPlan(plan)

	// Reports the plan (non-empty, names the orphan) — proves the default did work.
	if strings.TrimSpace(text) == "" {
		t.Fatalf("default dry-run produced empty plan text; expected a report for %q", orphan.TopicKey)
	}
	if !strings.Contains(text, orphan.TopicKey) {
		t.Errorf("default dry-run plan text does not report the orphan %q:\n%s", orphan.TopicKey, text)
	}

	if after := snapshotStore(t, store); !reflect.DeepEqual(before, after) {
		t.Errorf("default dry-run mutated observation rows / sync_ids:\nbefore = %v\n after = %v", before, after)
	}
	if afterFiles := snapshotFiles(t, skillsDir); !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Errorf("default dry-run mutated skill files:\nbefore = %v\n after = %v", beforeFiles, afterFiles)
	}
}

// Regression: an orphan that HAS candidate clusters renders each candidate on
// its own indented line as `    -> <label> (overlap <%.2f>)`, in the order the
// candidates appear in the OrphanPlan (PlanFrom/Suggest fix that order; the
// renderer must not re-sort). The Plan is built DIRECTLY from the structs — not
// via Suggest — so the assertion pins the exact rendered candidate-line shape
// (arrow, indent, two-decimal overlap) independent of the ranking core. This
// covers render.go's per-candidate branch, which no other test exercises.
func TestRenderPlan_RendersCandidateSuggestions(t *testing.T) {
	plan := Plan{Orphans: []OrphanPlan{
		{
			Topic: "skill/acme-orphan",
			Label: "acme-ghost",
			Candidates: []Candidate{
				{Label: "acme-high", Overlap: 0.6},
				{Label: "acme-mid", Overlap: 0.333333},
			},
		},
	}}

	want := "Reconcile plan: 1 orphaned skill record(s)\n" +
		"- skill/acme-orphan (bound label: acme-ghost)\n" +
		"    -> acme-high (overlap 0.60)\n" +
		"    -> acme-mid (overlap 0.33)\n"

	if got := RenderPlan(plan); got != want {
		t.Errorf("candidate-suggestion render mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Regression: the healthy/empty state. RenderPlan on a Plan with no orphans
// emits the single "memory is reconciled" line verbatim (and NOT the numbered
// header). This pins render.go's early-return branch, which no other test
// exercises.
func TestRenderPlan_EmptyPlan_HealthyState(t *testing.T) {
	want := "Reconcile plan: no orphaned skill records; memory is reconciled.\n"
	if got := RenderPlan(Plan{}); got != want {
		t.Errorf("healthy-state render mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
