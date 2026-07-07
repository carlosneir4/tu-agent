package main

import "path/filepath"

// pluginContext reports whether we are provisioning a repo that the Claude Code
// plugin already serves (its hooks.json supplies graph/memory hooks). Detection
// order: explicit --plugin flag, CLAUDE_PLUGIN_ROOT in the environment (set for
// skill-invoked runs), an installed plugin under ~/.claude/plugins.
func pluginContext(explicit bool, getenv func(string) string, home string) bool {
	if explicit {
		return true
	}
	if getenv("CLAUDE_PLUGIN_ROOT") != "" {
		return true
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "plugins", "*", "*", "tu-agent", "*"))
	return len(matches) > 0
}
