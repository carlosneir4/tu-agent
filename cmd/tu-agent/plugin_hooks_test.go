package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginHooksConfig mirrors plugin/hooks/hooks.json so a drift (a missing event
// or a renamed command) fails this test rather than silently breaking the
// installed plugin. The plugin must stay at parity with the CLI hooks emitted by
// internal/codegen.hardenHooks (see TestHardenedSettings* in that package).
type pluginHookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type pluginHookEntry struct {
	Matcher string          `json:"matcher,omitempty"`
	Hooks   []pluginHookCmd `json:"hooks"`
}

type pluginHooksConfig struct {
	Hooks map[string][]pluginHookEntry `json:"hooks"`
}

// eventCommands returns every command string registered under one hook event.
func (c pluginHooksConfig) eventCommands(event string) string {
	var cmds []string
	for _, e := range c.Hooks[event] {
		for _, h := range e.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	return strings.Join(cmds, "\n")
}

// loadPluginHooks reads and parses plugin/hooks/hooks.json.
func loadPluginHooks(t *testing.T) pluginHooksConfig {
	t.Helper()
	path := filepath.Join("..", "..", "plugin", "hooks", "hooks.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var cfg pluginHooksConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return cfg
}

func TestPluginHooksConfig(t *testing.T) {
	cfg := loadPluginHooks(t)

	// Each plugin hook invokes the binary through the shim.
	const prefix = "${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"

	cases := []struct {
		event string
		want  []string
	}{
		{"PostToolUse", []string{"graph update"}},
		{"SessionStart", []string{"graph update --quiet --announce", "memory import", "advise --nudge"}},
		{"Stop", []string{"memory export"}},
		{"SessionEnd", []string{"memory export"}},
		{"UserPromptSubmit", []string{"hook prompt-submit"}},
	}
	for _, tc := range cases {
		got := cfg.eventCommands(tc.event)
		if got == "" {
			t.Errorf("%s: no hook commands registered", tc.event)
			continue
		}
		if !strings.Contains(got, prefix) {
			t.Errorf("%s: commands must call the binary via %q; got %q", tc.event, prefix, got)
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q; got %q", tc.event, w, got)
			}
		}
	}

	// The session-orientation nudge (--announce) is a SessionStart-only concern:
	// PostToolUse fires on every Write/Edit/Bash, so re-printing the nudge there
	// would spam the agent's context on every tool call.
	if got := cfg.eventCommands("PostToolUse"); strings.Contains(got, "--announce") {
		t.Errorf("PostToolUse must not carry --announce (SessionStart-only); got %q", got)
	}

	// C7: the standalone `memory crystallize --nudge` hook command was
	// replaced by `advise --nudge` (advise's crystallize-ready rule absorbs
	// it) — a single deterministic suggestion channel on SessionStart.
	if got := cfg.eventCommands("SessionStart"); strings.Contains(got, "memory crystallize --nudge") {
		t.Errorf("SessionStart must no longer run the standalone crystallize nudge (superseded by advise --nudge); got %q", got)
	}
}

// TestPluginHooksDegradeSafely asserts every plugin hook command fails safe:
// each command line must end in " || exit 0" (so a version-skewed or missing
// binary never surfaces a hook error into the session), and none may force a
// network check every run via TU_AGENT_UPDATE_INTERVAL=0 (the shim's default
// 24h throttle must govern instead).
func TestPluginHooksDegradeSafely(t *testing.T) {
	cfg := loadPluginHooks(t)

	for event, entries := range cfg.Hooks {
		for _, entry := range entries {
			for _, h := range entry.Hooks {
				cmd := h.Command
				if !strings.HasSuffix(cmd, " || exit 0") {
					t.Errorf("%s: command %q must end in %q to degrade safely", event, cmd, " || exit 0")
				}
				if strings.Contains(cmd, "TU_AGENT_UPDATE_INTERVAL=0") {
					t.Errorf("%s: command %q must not force a check every run via TU_AGENT_UPDATE_INTERVAL=0", event, cmd)
				}
			}
		}
	}
}

// TestPluginHasPostBashHook asserts the plugin installs the Bash reconcile
// hook (Task 2), mirroring the CLI setup path's postToolUseHooks list.
func TestPluginHasPostBashHook(t *testing.T) {
	cfg := loadPluginHooks(t)
	found := false
	for _, e := range cfg.Hooks["PostToolUse"] {
		if e.Matcher != "Bash" {
			continue
		}
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "graph update --post-bash") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("plugin PostToolUse missing a Bash --post-bash hook")
	}
}

// TestPluginHasEditCheckHook asserts the plugin installs the
// edit-without-context behavioral hook alongside the graph update hook on the
// same Write|Edit PostToolUse matcher.
func TestPluginHasEditCheckHook(t *testing.T) {
	cfg := loadPluginHooks(t)
	found := false
	for _, e := range cfg.Hooks["PostToolUse"] {
		if e.Matcher != "Write|Edit" {
			continue
		}
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "hook edit-check") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("plugin PostToolUse (Write|Edit) missing a hook edit-check command")
	}
}
