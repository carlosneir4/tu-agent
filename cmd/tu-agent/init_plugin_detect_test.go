package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginContextPrecedence(t *testing.T) {
	empty := func(string) string { return "" }
	if !pluginContext(true, empty, t.TempDir()) {
		t.Errorf("explicit flag must win")
	}
	env := func(k string) string {
		if k == "CLAUDE_PLUGIN_ROOT" {
			return "/some/plugin"
		}
		return ""
	}
	if !pluginContext(false, env, t.TempDir()) {
		t.Errorf("CLAUDE_PLUGIN_ROOT must be detected")
	}
	home := t.TempDir()
	pdir := filepath.Join(home, ".claude", "plugins", "cache", "m", "tu-agent", "1.0.0")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !pluginContext(false, empty, home) {
		t.Errorf("installed plugin on disk must be detected")
	}
	if pluginContext(false, empty, t.TempDir()) {
		t.Errorf("bare environment must not detect a plugin")
	}
}
