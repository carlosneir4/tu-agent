package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// RED-phase tests for the SessionStart rules hook subcommand (@s5-@s8): the
// `hook session-rules` subcommand does not exist yet, so sessionRulesHookCmd
// fails t.Fatal at runtime (mirroring mcpActionHookCmd / promptExpansionCmd in
// the sibling *_hook_test.go files) until it is registered on hookCmd.

// sessionRulesHookCmd locates the `hook session-rules` subcommand on hookCmd
// at runtime. It does not exist on the pre-change tree, so this lookup — not
// a direct symbol reference — is the RED signal for @s5-@s7: the file
// compiles against the old tree and fails at runtime via t.Fatal.
func sessionRulesHookCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range hookCmd.Commands() {
		if c.Use == "session-rules" {
			return c
		}
	}
	t.Fatal("hook session-rules subcommand not registered on hookCmd")
	return nil
}

// newSessionRulesRepo creates a temp repo with a .git marker (so repoRoot()
// resolves inside it, not some ancestor repo) and chdirs into it for the
// test's duration. Mirrors newFlowEmittersRepo in flow_emitters_test.go.
func newSessionRulesRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Chdir(root)
	return root
}

// writeAllRules writes .tu-agent/rules/all.md under root with the given
// content, creating parent directories as needed.
func writeAllRules(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".tu-agent", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .tu-agent/rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "all.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write all.md: %v", err)
	}
}

// @s8 — session-rules subcommand is registered on hookCmd.
func TestSessionRulesHook_RegisteredOnHookCmd(t *testing.T) {
	// sessionRulesHookCmd itself t.Fatal's if not found; a plain lookup here
	// makes the scenario's assertion explicit and independent of @s5-@s7.
	c := sessionRulesHookCmd(t)
	if c.Use != "session-rules" {
		t.Errorf("@s8: subcommand Use = %q, want %q", c.Use, "session-rules")
	}
}

// @s5 — hook session-rules injects all.md into additionalContext.
func TestSessionRulesHook_InjectsRulesIntoAdditionalContext(t *testing.T) {
	root := newSessionRulesRepo(t)
	writeAllRules(t, root, "REPO-WIDE-RULE")

	c := sessionRulesHookCmd(t)
	var buf bytes.Buffer
	c.SetOut(&buf)

	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("@s5: RunE err = %v, want nil", err)
	}

	var got sessionStartHook
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("@s5: output not valid SessionStart hook JSON (%q): %v", buf.String(), err)
	}
	if got.HookSpecificOutput.AdditionalContext == "" {
		t.Fatal("@s5: additionalContext is empty, want it to contain the rules")
	}
	if want := "REPO-WIDE-RULE"; !bytes.Contains([]byte(got.HookSpecificOutput.AdditionalContext), []byte(want)) {
		t.Errorf("@s5: additionalContext = %q, want it to contain %q", got.HookSpecificOutput.AdditionalContext, want)
	}
}

// @s6 — hook session-rules is a no-op (zero bytes written) when there are no
// rules at all: no all.md file at the repo root.
func TestSessionRulesHook_NoRulesIsNoOp(t *testing.T) {
	newSessionRulesRepo(t) // .git only, no .tu-agent/rules/all.md

	c := sessionRulesHookCmd(t)
	var buf bytes.Buffer
	c.SetOut(&buf)

	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("@s6: RunE err = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("@s6: buffer = %q (%d bytes), want zero bytes written when there are no rules", buf.String(), buf.Len())
	}
}

// @s7 — hook session-rules emits no user-visible banner: systemMessage is
// empty even when rules ARE injected.
func TestSessionRulesHook_NoBannerSystemMessage(t *testing.T) {
	root := newSessionRulesRepo(t)
	writeAllRules(t, root, "REPO-WIDE-RULE")

	c := sessionRulesHookCmd(t)
	var buf bytes.Buffer
	c.SetOut(&buf)

	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("@s7: RunE err = %v, want nil", err)
	}

	var got sessionStartHook
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("@s7: output not valid SessionStart hook JSON (%q): %v", buf.String(), err)
	}
	if got.SystemMessage != "" {
		t.Errorf("@s7: systemMessage = %q, want empty (silent injection, no banner)", got.SystemMessage)
	}
}
