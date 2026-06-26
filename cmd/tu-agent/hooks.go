package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The PostToolUse hook both install paths deliver. The plugin path prefixes the
// command with ${CLAUDE_PLUGIN_ROOT}/bin/; the CLI path uses the bare command
// below (PATH-resolved).
const (
	hookMatcher = "Write|Edit"
	hookCommand = "tu-agent graph update --quiet"
)

// mergePostToolUseHook merges our PostToolUse hook into existing settings.json
// content. It returns the new content, whether a change was made, and any error.
// Other keys and other hooks are preserved (non-clobber); re-running is a no-op
// (idempotent) and returns the input unchanged with changed=false. It errors on
// malformed-but-parseable shapes to avoid silently discarding user data.
func mergePostToolUseHook(existing []byte) ([]byte, bool, error) {
	settings := map[string]any{}
	if t := bytes.TrimSpace(existing); len(t) > 0 {
		if err := json.Unmarshal(t, &settings); err != nil {
			return nil, false, fmt.Errorf("mergePostToolUseHook: parse settings: %w", err)
		}
	}

	// Be strict about malformed-but-parseable shapes: surface an error rather
	// than silently discarding the user's data or producing invalid output.
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

	for _, entry := range post {
		e, ok := entry.(map[string]any)
		if !ok || e["matcher"] != hookMatcher {
			continue
		}
		inner, ok := e["hooks"].([]any)
		if !ok {
			return nil, false, fmt.Errorf("mergePostToolUseHook: existing %q hook entry has a non-array \"hooks\"", hookMatcher)
		}
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok && hm["command"] == hookCommand {
				return existing, false, nil
			}
		}
	}

	post = append(post, map[string]any{
		"matcher": hookMatcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": hookCommand},
		},
	})
	hooks["PostToolUse"] = post
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("mergePostToolUseHook: marshal: %w", err)
	}
	return append(out, '\n'), true, nil
}
