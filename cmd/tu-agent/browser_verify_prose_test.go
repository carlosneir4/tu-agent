package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readPrepareSkillMD and readVerifyInEnvSkillMD mirror
// design_skill_test.go's readDesignSkillMD: two levels up from cmd/tu-agent
// to the repo root, then into plugin/skills. Fatalf is the honest-red signal
// only if the SKILL.md file itself goes missing — the pinned prose additions
// this file checks for are asserted with t.Error inside each test body, since
// the files already exist today and only lack the new sections.
//
// sectionBetween (section-scoping helper) is reused as-is from
// design_skill_test.go — it lives in this package already, no need to
// redeclare it.

func readPrepareSkillMD(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "prepare", "SKILL.md"))
	if err != nil {
		t.Fatalf("read prepare SKILL.md: %v", err)
	}
	return string(raw)
}

func readVerifyInEnvSkillMD(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "verify-in-env", "SKILL.md"))
	if err != nil {
		t.Fatalf("read verify-in-env SKILL.md: %v", err)
	}
	return string(raw)
}

// prepareOfferSection scopes to the new offer step design.md says must land
// between the existing "Step 1: Deterministic setup" and "Step 2: Ensure the
// concept store is populated" headings — both already present today, so this
// never Fatals; it just returns a section that (pre-GREEN) does not yet
// contain the offer prose, giving an honest t.Error red.
func prepareOfferSection(t *testing.T, s string) string {
	t.Helper()
	return sectionBetween(t, s, "## Step 1: Deterministic setup (binary)", "## Step 2: Ensure the concept store is populated")
}

// verifyInEnvBrowserSection scopes to sections 3-4 ("Drive the changed flow"
// through the start of "Persist the recipe"), the range design.md calls the
// drive section where the browser path, the per-flow gate, and the audit
// trail all land.
func verifyInEnvBrowserSection(t *testing.T, s string) string {
	t.Helper()
	return sectionBetween(t, s, `## 3. Drive the changed flow — not just "the app starts"`, "## 5. Persist the recipe")
}

// @s1 — prepare's offer step runs `playwright detect` and gates on the
// three-part condition, pinned exactly as design.md states it.
func TestPrepareOfferStepInvokesDetectAndGates(t *testing.T) {
	s := readPrepareSkillMD(t)
	section := prepareOfferSection(t, s)

	if !strings.Contains(section, "playwright detect") {
		t.Error(`prepare offer step missing the "playwright detect" invocation`)
	}
	if !strings.Contains(section, "web==true && declined==false && enabled==false") {
		t.Error("prepare offer step missing the pinned gate condition `web==true && declined==false && enabled==false`")
	}
}

// @s2 — the offer states what gets written (.mcp.json entry origin-locked to
// localhost, the settings allowlist/permission) and warns about the
// capability before asking for the app's dev port.
func TestPrepareOfferStepStatesCapabilityAndAsksPort(t *testing.T) {
	s := readPrepareSkillMD(t)
	section := prepareOfferSection(t, s)

	for _, want := range []string{
		"origin-locked",
		"localhost",
		"allowlist",
		"mcp__playwright",
		"a browser acting on the local environment",
		"dev port",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("prepare offer step missing %q", want)
		}
	}
}

// @s3 — declining runs `playwright decline`, is never re-nagged, and non-web
// repos get no offer and no mention at all.
func TestPrepareOfferStepDeclineNeverRenagsAndSilentOnNonWeb(t *testing.T) {
	s := readPrepareSkillMD(t)
	section := prepareOfferSection(t, s)

	if !strings.Contains(section, "playwright decline") {
		t.Error(`prepare offer step missing the "playwright decline" invocation on "no"`)
	}
	if !strings.Contains(section, "never re-nagged") {
		t.Error(`prepare offer step missing the pinned "never re-nagged" phrase`)
	}
	if !strings.Contains(section, "no offer and no mention") {
		t.Error(`prepare offer step missing the pinned "no offer and no mention" phrase for non-web repos`)
	}
}

// @s4 — the browser path checks for Playwright MCP tools via ToolSearch and
// keeps the existing CLI/curl fallback path intact when they are absent.
func TestVerifyInEnvBrowserPathToolSearchWithFallback(t *testing.T) {
	s := readVerifyInEnvSkillMD(t)
	section := verifyInEnvBrowserSection(t, s)

	if !strings.Contains(section, `ToolSearch "playwright"`) {
		t.Error(`verify-in-env drive section missing the pinned ToolSearch "playwright" presence check`)
	}
	if !strings.Contains(section, "CLI/curl") {
		t.Error("verify-in-env drive section missing the CLI/curl fallback mention")
	}
	if !strings.Contains(section, "fallback intact") {
		t.Error(`verify-in-env drive section missing the pinned "fallback intact" phrase`)
	}
}

// @s5 — the per-flow human gate presents numbered steps approved once, and
// anything outside the approved flow (new origin, destructive action, real
// credentials) requires a fresh gate.
func TestVerifyInEnvPerFlowGateNumberedStepsFreshGate(t *testing.T) {
	s := readVerifyInEnvSkillMD(t)
	section := verifyInEnvBrowserSection(t, s)

	if !strings.Contains(section, "open /login, fill the form with test data, assert the redirect") {
		t.Error("verify-in-env drive section missing the pinned numbered-steps example")
	}
	if !strings.Contains(section, "approves ONCE") {
		t.Error(`verify-in-env drive section missing the pinned "approves ONCE" phrase`)
	}
	for _, trigger := range []string{
		"a new origin",
		"a destructive action (deleting data)",
		"real credentials",
	} {
		if !strings.Contains(section, trigger) {
			t.Errorf("verify-in-env drive section missing fresh-gate trigger %q", trigger)
		}
	}
	if !strings.Contains(section, "FRESH gate") {
		t.Error(`verify-in-env drive section missing the pinned "FRESH gate" phrase`)
	}
}

// @s6 — every browser action is an MCP call recorded by telemetry (the audit
// trail), with screenshots or DOM assertions named as the evidence.
func TestVerifyInEnvTelemetryAuditTrailAndEvidence(t *testing.T) {
	s := readVerifyInEnvSkillMD(t)
	section := verifyInEnvBrowserSection(t, s)

	if !strings.Contains(section, "recorded by telemetry") {
		t.Error(`verify-in-env drive section missing the pinned "recorded by telemetry" phrase`)
	}
	if !strings.Contains(section, "audit trail") {
		t.Error(`verify-in-env drive section missing the pinned "audit trail" phrase`)
	}
	if !strings.Contains(section, "screenshots") {
		t.Error("verify-in-env drive section missing the screenshots evidence mention")
	}
	if !strings.Contains(section, "DOM") {
		t.Error("verify-in-env drive section missing the DOM evidence mention")
	}
}
