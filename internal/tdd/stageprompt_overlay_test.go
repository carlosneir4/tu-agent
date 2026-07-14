package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/subagent"
)

// writeOverlayAgent materializes a minimal .claude/agents/<role>.md with YAML
// frontmatter and a sentinel body line. stripFrontmatter yields exactly the
// sentinel (no trailing newline), so the composed body is byte-predictable.
// It returns the sentinel so tests can locate the body in the composed prompt.
func writeOverlayAgent(t *testing.T, root, role string) string {
	t.Helper()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	sentinel := "SENTINEL_BODY_" + role
	content := "---\nname: " + role + "\n---\n" + sentinel
	if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write agent %s: %v", role, err)
	}
	return sentinel
}

// writeOverlayGoConfig writes .tu-agent/config.yaml pinning the resolved overlay
// language to "go" so GREEN's root-based resolution yields "go".
func writeOverlayGoConfig(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".tu-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .tu-agent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("tdd:\n  language: go\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// writeOverlayRules writes .tu-agent/rules.md containing sentinel so the composed
// prompt carries a locatable project-rules section.
func writeOverlayRules(t *testing.T, root, sentinel string) {
	t.Helper()
	dir := filepath.Join(root, ".tu-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .tu-agent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.md"), []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write rules.md: %v", err)
	}
}

// @s1: the go/developer overlay is injected between the agent body and the
// generic TDD overlay for the craftsman (developer role) stage.
func TestComposeStagePrompt_S1_InjectsGoDeveloperOverlay(t *testing.T) {
	root := t.TempDir()
	bodySentinel := writeOverlayAgent(t, root, "developer")
	writeOverlayGoConfig(t, root)
	base := TddRelBase("", "x")

	out, err := ComposeStagePrompt(root, "craftsman", base)
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}

	overlay := codegen.LangOverlay("go", "developer")
	if overlay == "" {
		t.Fatal("precondition: codegen.LangOverlay(\"go\",\"developer\") must be non-empty")
	}
	tddOverlay := WithBaseDir(CraftsmanPrompt, base)

	iBody := strings.Index(out, bodySentinel)
	iOverlay := strings.Index(out, overlay)
	iTdd := strings.Index(out, tddOverlay)

	if iBody < 0 {
		t.Fatalf("agent body sentinel %q not found in output", bodySentinel)
	}
	if iTdd < 0 {
		t.Fatal("generic TDD craftsman overlay not found in output")
	}
	// RED: the language overlay is not injected today, so iOverlay is -1.
	if iOverlay < 0 {
		t.Fatalf("go/developer language overlay not injected into composed prompt (RED)")
	}
	if !(iBody < iOverlay && iOverlay < iTdd) {
		t.Fatalf("order wrong: want body(%d) < overlay(%d) < tddOverlay(%d)", iBody, iOverlay, iTdd)
	}
}

// @s2: composition order is body -> language overlay -> project rules -> tdd
// overlay, with exactly one blank line between adjacent sections.
func TestComposeStagePrompt_S2_OrderAndNoDoubledBlanks(t *testing.T) {
	root := t.TempDir()
	bodySentinel := writeOverlayAgent(t, root, "developer")
	writeOverlayGoConfig(t, root)
	rulesSentinel := "SENTINEL_RULES_BLOCK"
	writeOverlayRules(t, root, rulesSentinel)
	base := TddRelBase("", "x")

	out, err := ComposeStagePrompt(root, "craftsman", base)
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}

	overlay := codegen.LangOverlay("go", "developer")
	if overlay == "" {
		t.Fatal("precondition: codegen.LangOverlay(\"go\",\"developer\") must be non-empty")
	}
	tddOverlay := WithBaseDir(CraftsmanPrompt, base)

	iBody := strings.Index(out, bodySentinel)
	iOverlay := strings.Index(out, overlay)
	iRules := strings.Index(out, rulesSentinel)
	iTdd := strings.Index(out, tddOverlay)

	if iBody < 0 {
		t.Fatalf("agent body sentinel %q not found", bodySentinel)
	}
	if iRules < 0 {
		t.Fatalf("project rules sentinel %q not found", rulesSentinel)
	}
	if iTdd < 0 {
		t.Fatal("generic TDD craftsman overlay not found")
	}
	// RED: overlay absent today.
	if iOverlay < 0 {
		t.Fatalf("go/developer language overlay not injected into composed prompt (RED)")
	}
	if !(iBody < iOverlay && iOverlay < iRules && iRules < iTdd) {
		t.Fatalf("order wrong: want body(%d) < overlay(%d) < rules(%d) < tddOverlay(%d)", iBody, iOverlay, iRules, iTdd)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("output has doubled blank lines (\\n\\n\\n); sections must join with a single blank line")
	}
}

// @s3: analyst has no language overlay (go/analyst.md does not exist), so the
// composition is byte-identical to the pre-feature form body + "\n\n" + overlay.
func TestComposeStagePrompt_S3_AnalystNoLanguageOverlay(t *testing.T) {
	root := t.TempDir()
	bodySentinel := writeOverlayAgent(t, root, "analyst")
	writeOverlayGoConfig(t, root)
	base := TddRelBase("", "x")

	if codegen.LangOverlay("go", "analyst") != "" {
		t.Fatal("precondition: codegen.LangOverlay(\"go\",\"analyst\") must be empty")
	}

	out, err := ComposeStagePrompt(root, "analyst", base)
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}

	// No rules.md written: expected form is body + "\n\n" + tddOverlay.
	expected := bodySentinel + "\n\n" + WithBaseDir(AnalystPrompt, base)
	if out != expected {
		t.Fatalf("analyst composition changed.\n got: %q\nwant: %q", out, expected)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("output has doubled blank lines")
	}
}

// @s4: an unresolved language (no config.yaml, no build tool) composes exactly
// as today with no language overlay section.
func TestComposeStagePrompt_S4_UnresolvedLanguage(t *testing.T) {
	root := t.TempDir()
	writeOverlayAgent(t, root, "architect")
	// Deliberately NO .tu-agent/config.yaml and no go.mod/pom.xml/package.json,
	// so the resolved language is "".
	base := TddRelBase("", "x")

	out, err := ComposeStagePrompt(root, "architect", base)
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}

	goArchOverlay := codegen.LangOverlay("go", "architect")
	if goArchOverlay != "" && strings.Contains(out, goArchOverlay) {
		t.Fatalf("output must not contain the go/architect overlay when language is unresolved")
	}
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("output has doubled blank lines")
	}
}

// @s5: the frozen TddStageDefs path is unchanged — the architect definition's
// SystemPrompt is body + "\n\n" + tdd overlay, with no language overlay injected.
func TestTddStageDefs_S5_ArchitectFrozenNoLanguageOverlay(t *testing.T) {
	root := t.TempDir()
	writeOverlayGoConfig(t, root) // even with go resolvable, the frozen path must not inject.
	for _, role := range []string{"architect", "developer", "pr-reviewer", "scribe"} {
		writeOverlayAgent(t, root, role)
	}
	base := TddRelBase("", "x")

	defs, err := TddStageDefs(root, base)
	if err != nil {
		t.Fatalf("tddStageDefs: %v", err)
	}

	var arch *subagent.Definition
	for _, d := range defs {
		if d.Name == "architect" {
			arch = d
			break
		}
	}
	if arch == nil {
		t.Fatal("architect definition not found in tddStageDefs output")
	}

	expected := "SENTINEL_BODY_architect" + "\n\n" + WithBaseDir(ArchitectPrompt, base)
	if arch.SystemPrompt != expected {
		t.Fatalf("architect SystemPrompt changed.\n got: %q\nwant: %q", arch.SystemPrompt, expected)
	}
}
