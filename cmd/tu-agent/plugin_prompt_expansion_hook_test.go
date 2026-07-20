package main

import (
	"strings"
	"testing"
)

// TestPluginHooksConfig_PromptExpansion pins @s5: hooks.json wires the
// prompt-expansion hidden subcommand under a UserPromptExpansion event with
// the fail-safe " || exit 0" guard, and the existing UserPromptSubmit entry
// stays wired to hook prompt-submit unchanged. Compiles against the
// pre-change hooks.json via the existing loadPluginHooks/eventCommands
// helpers (plugin_hooks_test.go, same package); RED because
// eventCommands("UserPromptExpansion") is empty until the event is added.
func TestPluginHooksConfig_PromptExpansion(t *testing.T) {
	cfg := loadPluginHooks(t)

	got := cfg.eventCommands("UserPromptExpansion")
	if got == "" {
		t.Fatal("UserPromptExpansion: no hook commands registered")
	}
	if !strings.Contains(got, "hook prompt-expansion") {
		t.Errorf("UserPromptExpansion: missing \"hook prompt-expansion\"; got %q", got)
	}
	if !strings.HasSuffix(got, " || exit 0") {
		t.Errorf("UserPromptExpansion: command must end in \" || exit 0\"; got %q", got)
	}

	submit := cfg.eventCommands("UserPromptSubmit")
	if !strings.Contains(submit, "hook prompt-submit") {
		t.Errorf("UserPromptSubmit: missing \"hook prompt-submit\"; got %q", submit)
	}
}
