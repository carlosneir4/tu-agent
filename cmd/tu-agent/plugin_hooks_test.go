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
		{"SessionStart", []string{"graph update", "memory import"}},
		{"Stop", []string{"memory export"}},
		{"SessionEnd", []string{"memory export"}},
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
