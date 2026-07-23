package main

import (
	"strings"
	"testing"
)

// @s9 — hooks.json wires session-rules into SessionStart, separately from the
// existing advise --nudge entry: both must be present, as distinct command
// strings. Reuses loadPluginHooks / pluginHooksConfig from
// plugin_hooks_test.go (same package) rather than redefining the loader.
func TestPluginHooksConfig_SessionRulesWiredSeparatelyFromAdvise(t *testing.T) {
	cfg := loadPluginHooks(t)

	var rulesCmd, advisedCmd string
	for _, e := range cfg.Hooks["SessionStart"] {
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "hook session-rules") {
				rulesCmd = h.Command
			}
			if strings.Contains(h.Command, "advise --nudge") {
				advisedCmd = h.Command
			}
		}
	}

	if rulesCmd == "" {
		t.Fatal(`@s9: SessionStart has no entry running "hook session-rules"`)
	}
	if advisedCmd == "" {
		t.Fatal(`@s9: SessionStart has no entry running "advise --nudge" (expected to remain present)`)
	}
	if rulesCmd == advisedCmd {
		t.Errorf("@s9: \"hook session-rules\" and \"advise --nudge\" must be distinct command entries, both resolved to %q", rulesCmd)
	}
}
