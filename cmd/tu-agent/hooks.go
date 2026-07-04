package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ptuHook is one PostToolUse hook: a tool-name matcher and the command to run.
type ptuHook struct{ matcher, command string }

// postToolUseHooks are the tu-agent PostToolUse hooks the setup path installs.
// Write|Edit reconciles after every edit; Bash reconciles only after a
// tree-mutating command (rm/git checkout/…) via `graph update --post-bash`.
// The plugin path (plugin/hooks/hooks.json) mirrors these with a bin/ prefix.
var postToolUseHooks = []ptuHook{
	{matcher: "Write|Edit", command: "tu-agent graph update --quiet"},
	{matcher: "Bash", command: "tu-agent graph update --post-bash"},
}

// mergePostToolUseHook merges the tu-agent PostToolUse hooks into existing
// settings.json content. Other keys and other hooks are preserved (non-clobber);
// re-running is idempotent (returns the input unchanged with changed=false). It
// errors on malformed-but-parseable shapes to avoid silently discarding data.
func mergePostToolUseHook(existing []byte) ([]byte, bool, error) {
	settings := map[string]any{}
	if t := bytes.TrimSpace(existing); len(t) > 0 {
		if err := json.Unmarshal(t, &settings); err != nil {
			return nil, false, fmt.Errorf("mergePostToolUseHook: parse settings: %w", err)
		}
	}
	if raw, ok := settings["hooks"]; ok {
		if _, ok := raw.(map[string]any); !ok {
			return nil, false, fmt.Errorf(`mergePostToolUseHook: existing "hooks" is not a JSON object`)
		}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	if raw, ok := hooks["PostToolUse"]; ok {
		if _, ok := raw.([]any); !ok {
			return nil, false, fmt.Errorf(`mergePostToolUseHook: existing "hooks.PostToolUse" is not a JSON array`)
		}
	}
	post, _ := hooks["PostToolUse"].([]any)

	changed := false
	for _, want := range postToolUseHooks {
		has, err := postToolUseHasCommand(post, want)
		if err != nil {
			return nil, false, err
		}
		if has {
			continue
		}
		post = append(post, map[string]any{
			"matcher": want.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": want.command}},
		})
		changed = true
	}
	if !changed {
		return existing, false, nil
	}
	hooks["PostToolUse"] = post
	settings["hooks"] = hooks
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("mergePostToolUseHook: marshal: %w", err)
	}
	return append(out, '\n'), true, nil
}

// postToolUseHasCommand reports whether post already contains an entry whose
// matcher and command match want. It errors on a malformed entry shape.
func postToolUseHasCommand(post []any, want ptuHook) (bool, error) {
	for _, entry := range post {
		e, ok := entry.(map[string]any)
		if !ok || e["matcher"] != want.matcher {
			continue
		}
		inner, ok := e["hooks"].([]any)
		if !ok {
			return false, fmt.Errorf("mergePostToolUseHook: existing %q hook entry has a non-array \"hooks\"", want.matcher)
		}
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok && hm["command"] == want.command {
				return true, nil
			}
		}
	}
	return false, nil
}
