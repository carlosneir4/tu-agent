package main

// RED-phase tests for feature design-approval-token (spec.md D4, design.md
// Feature 1): a new `tdd state approve-design --base <base> --rounds N`
// subcommand writes progress/design-approval.json (identity/timestamp/rounds)
// and stamps plan.md's `## Sign-off` section; `tdd state begin` gains a
// `--complexity` flag that refuses standard/complex/empty begins without a
// prior approval token, while `trivial` never requires one.
//
// None of that exists yet: approve-design is not registered on tddStateCmd,
// and --complexity is not a registered flag on tddStateBeginCmd. Every test
// below drives the real cobra command objects at runtime (never a compile-time
// reference to the missing subcommand/flag), the same pattern as
// TestFlowEmittersS4_StateReviewWithFindingsWritesBranchReviewRow in
// flow_emitters_test.go: a nil command lookup or a failed Flags().Set is an
// honest RUNTIME red, not a build failure.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newDesignApprovalRepo creates a temp repo with a real `git init` and a
// local user.email (isolating the test from the host's global git config,
// since approve-design's approver identity is sourced from `git config
// user.email`), then chdirs into it for the test's duration.
func newDesignApprovalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitIn(t, root, "init")
	runGitIn(t, root, "config", "user.email", "tester@example.com")
	runGitIn(t, root, "config", "user.name", "Tester")
	t.Chdir(root)
	return root
}

// findApproveDesignCmd walks tddStateCmd's registered subcommands looking for
// one whose Use starts with "approve-design". Returns nil today (the
// subcommand does not exist yet) — callers must treat nil as the RED signal,
// not a setup bug.
func findApproveDesignCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range tddStateCmd.Commands() {
		if strings.HasPrefix(c.Use, "approve-design") {
			return c
		}
	}
	return nil
}

// runApproveDesign sets --base/--rounds at runtime and executes the found
// approve-design command's RunE.
func runApproveDesign(t *testing.T, cmd *cobra.Command, base, rounds string) {
	t.Helper()
	if err := cmd.Flags().Set("base", base); err != nil {
		t.Fatalf("set --base: %v", err)
	}
	if err := cmd.Flags().Set("rounds", rounds); err != nil {
		t.Fatalf("set --rounds: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("tdd state approve-design --base %s --rounds %s: %v", base, rounds, err)
	}
}

// readApprovalToken reads and JSON-decodes progress/design-approval.json
// under base into a generic map, so field assertions do not depend on a
// production Go type that does not exist yet.
func readApprovalToken(t *testing.T, base string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(base, "progress", "design-approval.json"))
	if err != nil {
		t.Fatalf("read design-approval.json: %v", err)
	}
	var token map[string]any
	if err := json.Unmarshal(data, &token); err != nil {
		t.Fatalf("design-approval.json is not valid JSON: %v\n%s", err, data)
	}
	return token
}

// @s1: approve-design writes the token with identity, timestamp and rounds.
func TestDesignApprovalS1_ApproveDesignWritesToken(t *testing.T) {
	root := newDesignApprovalRepo(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run1")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	approveCmd := findApproveDesignCmd(t)
	if approveCmd == nil {
		t.Fatalf("tdd state approve-design subcommand does not exist yet on tddStateCmd")
	}
	runApproveDesign(t, approveCmd, base, "2")

	token := readApprovalToken(t, base)
	if _, ok := token["approved_by"]; !ok {
		t.Errorf("token missing key approved_by: %+v", token)
	}
	if _, ok := token["approved_at"]; !ok {
		t.Errorf("token missing key approved_at: %+v", token)
	}
	if rounds, ok := token["rounds"]; !ok || rounds != float64(2) {
		t.Errorf("token rounds = %v (present=%v), want 2: %+v", rounds, ok, token)
	}
}

// @s2: re-running approve-design overwrites the token (rounds updates from 1
// to 3, not accumulates or errors).
func TestDesignApprovalS2_ApproveDesignOverwritesOnRerun(t *testing.T) {
	root := newDesignApprovalRepo(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run2")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	approveCmd := findApproveDesignCmd(t)
	if approveCmd == nil {
		t.Fatalf("tdd state approve-design subcommand does not exist yet on tddStateCmd")
	}
	runApproveDesign(t, approveCmd, base, "1")
	runApproveDesign(t, approveCmd, base, "3")

	token := readApprovalToken(t, base)
	if rounds, ok := token["rounds"]; !ok || rounds != float64(3) {
		t.Errorf("token rounds after re-approval = %v (present=%v), want 3: %+v", rounds, ok, token)
	}
}

// @s3: `tdd state begin --complexity standard` with no approval token is
// refused, names the remediation, and leaves no state.json on disk.
func TestDesignApprovalS3_BeginStandardWithoutTokenRefused(t *testing.T) {
	root := newDesignApprovalRepo(t)
	resetTddStateFlags(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run3")

	tddStateBaseFlag = base
	tddStateFeatures = []string{"f1"}
	tddStateTask = "t"
	if err := tddStateBeginCmd.Flags().Set("complexity", "standard"); err != nil {
		t.Logf("--complexity not yet registered on tdd state begin (expected pre-implementation): %v", err)
	}

	err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil)
	if err == nil {
		t.Fatalf("expected begin with --complexity standard and no approval token to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "design not approved") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "design not approved")
	}
	if !strings.Contains(err.Error(), "tdd state approve-design") {
		t.Errorf("error = %q, want it to name the remediation %q", err.Error(), "tdd state approve-design")
	}
	if _, statErr := os.Stat(filepath.Join(base, "state.json")); statErr == nil {
		t.Errorf("state.json was written under %s despite the refused begin", base)
	}
}

// @s4: `tdd state begin --complexity standard` with a valid approval token
// proceeds and records the complexity in state.json.
func TestDesignApprovalS4_BeginStandardWithTokenProceeds(t *testing.T) {
	root := newDesignApprovalRepo(t)
	resetTddStateFlags(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run4")
	if err := os.MkdirAll(filepath.Join(base, "progress"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Fixture: approve-design already ran — written directly as the raw JSON
	// shape approve-design produces (design.md's exact token shape), not by
	// invoking the not-yet-existing subcommand.
	tokenRaw := `{"approved_by":"tester@example.com","approved_at":"2026-07-18T00:00:00Z","rounds":1}`
	if err := os.WriteFile(filepath.Join(base, "progress", "design-approval.json"), []byte(tokenRaw), 0o644); err != nil {
		t.Fatal(err)
	}

	tddStateBaseFlag = base
	tddStateFeatures = []string{"f1"}
	tddStateTask = "t"
	if err := tddStateBeginCmd.Flags().Set("complexity", "standard"); err != nil {
		t.Logf("--complexity not yet registered on tdd state begin (expected pre-implementation): %v", err)
	}

	if err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil); err != nil {
		t.Fatalf("expected begin with --complexity standard and a valid token to succeed, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(base, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if !strings.Contains(string(data), `"complexity": "standard"`) {
		t.Errorf("state.json missing complexity: standard, got: %s", data)
	}
}

// @s5 (REVISED — old-conductor compat): `tdd state begin` with NO
// --complexity flag at all SUCCEEDS with no approval token required — this
// is the compat path for a conductor that predates --complexity. It prints a
// loud warning naming the missing complexity and pointing at the new
// conductor flow, and writes state.json WITHOUT a complexity field. Explicit
// standard/complex (see @s3/@s4) stays fail-closed; only the flag's total
// absence takes this compat path. Red today: the warning text does not exist
// in the command's output (state.json already omits an unset complexity
// field today, trivially, since the field itself does not exist yet).
func TestDesignApprovalS5_BeginWithoutComplexityFlagWarnsAndProceeds(t *testing.T) {
	root := newDesignApprovalRepo(t)
	resetTddStateFlags(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run5")

	tddStateBaseFlag = base
	tddStateFeatures = []string{"f1"}
	tddStateTask = "t"
	// Deliberately no --complexity Set call at all.

	out, err := captureStdout(t, func() error {
		return tddStateBeginCmd.RunE(tddStateBeginCmd, nil)
	})
	if err != nil {
		t.Fatalf("expected begin with no --complexity flag to succeed (old-conductor compat), got: %v", err)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "complexity") {
		t.Errorf("expected a warning naming the missing complexity, got output: %q", out)
	}
	if !strings.Contains(lower, "conductor") {
		t.Errorf("expected the warning to point at the new conductor flow, got output: %q", out)
	}

	data, err := os.ReadFile(filepath.Join(base, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if strings.Contains(string(data), `"complexity"`) {
		t.Errorf("state.json should omit complexity entirely when no --complexity flag was given, got: %s", data)
	}
}

// @s6: `tdd state begin --complexity trivial` never requires a token and
// records complexity "trivial" in state.json.
func TestDesignApprovalS6_BeginTrivialNeverRequiresToken(t *testing.T) {
	root := newDesignApprovalRepo(t)
	resetTddStateFlags(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run6")

	tddStateBaseFlag = base
	tddStateFeatures = []string{"f1"}
	tddStateTask = "t"
	if err := tddStateBeginCmd.Flags().Set("complexity", "trivial"); err != nil {
		t.Logf("--complexity not yet registered on tdd state begin (expected pre-implementation): %v", err)
	}

	if err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil); err != nil {
		t.Fatalf("expected begin with --complexity trivial to succeed without a token, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(base, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if !strings.Contains(string(data), `"complexity": "trivial"`) {
		t.Errorf("state.json missing complexity: trivial, got: %s", data)
	}
}

// @s7: approve-design stamps a `## Sign-off` section into an existing
// plan.md — replacing the pending empty section, never duplicating it.
func TestDesignApprovalS7_ApproveDesignStampsSignOff(t *testing.T) {
	root := newDesignApprovalRepo(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run7")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	planContent := "# Plan\n\n## Summary\n\nSomething.\n\n## Sign-off\n\n"
	if err := os.WriteFile(filepath.Join(base, "plan.md"), []byte(planContent), 0o644); err != nil {
		t.Fatal(err)
	}

	approveCmd := findApproveDesignCmd(t)
	if approveCmd == nil {
		t.Fatalf("tdd state approve-design subcommand does not exist yet on tddStateCmd")
	}
	runApproveDesign(t, approveCmd, base, "1")

	data, err := os.ReadFile(filepath.Join(base, "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Approved by") {
		t.Errorf("plan.md missing a line starting with %q, got:\n%s", "Approved by", content)
	}
	if got := strings.Count(content, "## Sign-off"); got != 1 {
		t.Errorf("plan.md has %d \"## Sign-off\" headings after approval, want exactly 1:\n%s", got, content)
	}
}

// Review-fix round, item 2 (no @s tag): approve-design must leave a
// tdd_stage telemetry row (Stage "approve-design"), matching the existing
// begin/mark/review flow-event convention (recordTddStage in
// cmd/tu-agent/telemetry_events.go). Red today: the subcommand does not
// exist, so no row is ever written.
func TestDesignApprovalTelemetry_ApproveDesignEmitsTddStageRow(t *testing.T) {
	root := newDesignApprovalRepo(t)
	withTelemetryLevel(t, "minimal")
	base := filepath.Join(root, ".tu-agent", "tdd", "run-telemetry")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	approveCmd := findApproveDesignCmd(t)
	if approveCmd == nil {
		t.Fatalf("tdd state approve-design subcommand does not exist yet on tddStateCmd")
	}
	runApproveDesign(t, approveCmd, base, "1")

	row := findTelemetryRow(t, root, `"event":"tdd_stage"`)
	if !strings.Contains(row, `"stage":"approve-design"`) {
		t.Errorf("tdd_stage row must carry stage=approve-design: %s", row)
	}
}

// Review-fix round, item 3 (no @s tag): a token file that IS present on disk
// but carries no (or empty) approved_by must NOT satisfy the approval
// precondition — an audit token without identity is no approval (design.md's
// G3 rationale, mirrored from requireAuthor's hard-error contract). Red
// today: the enforcement itself does not exist yet, so begin proceeds
// regardless of any token content.
func TestDesignApprovalToken_EmptyIdentityDoesNotSatisfyGate(t *testing.T) {
	root := newDesignApprovalRepo(t)
	resetTddStateFlags(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run-empty-identity")
	if err := os.MkdirAll(filepath.Join(base, "progress"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A parseable-but-empty token: {} carries no approved_by at all.
	if err := os.WriteFile(filepath.Join(base, "progress", "design-approval.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tddStateBaseFlag = base
	tddStateFeatures = []string{"f1"}
	tddStateTask = "t"
	if err := tddStateBeginCmd.Flags().Set("complexity", "standard"); err != nil {
		t.Logf("--complexity not yet registered on tdd state begin (expected pre-implementation): %v", err)
	}

	err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil)
	if err == nil {
		t.Fatalf("expected begin with --complexity standard and an empty-identity token ({}) to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "design not approved") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "design not approved")
	}
}

// Review-fix round, item 4 (no @s tag): approve-design's Sign-off
// replace-or-append must not be fooled by a heading that merely starts with
// "## Sign-off" as a text prefix (e.g. "## Sign-off notes" — a DIFFERENT,
// unrelated section) nor treat an "### Sign-off" (h3) as the h2 gate
// section. Both must survive byte-for-byte, and exactly one exact "##
// Sign-off" h2 heading must exist afterward. Red today: the subcommand does
// not exist, which would also fail a naive `\n## Sign-off` prefix-scan
// implementation this test is written to catch once one exists.
func TestDesignApprovalSignOff_EdgeCasePrefixAndH3Collision(t *testing.T) {
	root := newDesignApprovalRepo(t)
	base := filepath.Join(root, ".tu-agent", "tdd", "run-signoff-edge")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	planContent := "# Plan\n\n" +
		"## Sign-off notes\n\n" +
		"Keep this section — it is not the Sign-off gate, just similarly named.\n\n" +
		"## Design\n\n" +
		"### Sign-off\n\n" +
		"An h3 subsection that must not be mistaken for the h2 gate section.\n\n" +
		"## Sign-off\n\n"
	if err := os.WriteFile(filepath.Join(base, "plan.md"), []byte(planContent), 0o644); err != nil {
		t.Fatal(err)
	}

	approveCmd := findApproveDesignCmd(t)
	if approveCmd == nil {
		t.Fatalf("tdd state approve-design subcommand does not exist yet on tddStateCmd")
	}
	runApproveDesign(t, approveCmd, base, "1")

	data, err := os.ReadFile(filepath.Join(base, "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "## Sign-off notes") {
		t.Errorf("the unrelated \"## Sign-off notes\" section heading was corrupted:\n%s", content)
	}
	if !strings.Contains(content, "Keep this section") {
		t.Errorf("the unrelated \"## Sign-off notes\" section body was lost:\n%s", content)
	}
	if !strings.Contains(content, "### Sign-off") {
		t.Errorf("the h3 \"### Sign-off\" subsection heading was corrupted:\n%s", content)
	}
	if !strings.Contains(content, "An h3 subsection") {
		t.Errorf("the h3 \"### Sign-off\" subsection body was lost:\n%s", content)
	}
	if !strings.Contains(content, "Approved by") {
		t.Errorf("plan.md missing a line starting with %q, got:\n%s", "Approved by", content)
	}
	// Exact line match isolates the true h2 "## Sign-off" heading from both
	// collisions: "## Sign-off notes" fails an exact match (extra text), and
	// "### Sign-off" fails one too (extra leading #).
	h2Count := 0
	for _, line := range strings.Split(content, "\n") {
		if line == "## Sign-off" {
			h2Count++
		}
	}
	if h2Count != 1 {
		t.Errorf("plan.md has %d exact \"## Sign-off\" h2 headings, want exactly 1:\n%s", h2Count, content)
	}
}
