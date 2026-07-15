package codegen

import (
	"encoding/json"
	"strings"
)

// stripRenamedWriteRules drops Write(path) entries from existing when generated
// carries the Edit(path) twin. tu-agent used to spell its write-side file rules
// Write(...); Claude Code matches file-permission rules only against Edit(...),
// so those entries are inert. Because MergeSettings unions rather than replaces,
// a repo hardened before the rename would otherwise keep the dead rules through
// every upgrade.
//
// The generated Edit twin is what makes a drop safe, so it is also what gates
// it: a Write(...) rule tu-agent never emitted has no twin and survives
// untouched — inert or not, it is the user's to keep. And the permission
// surface can never widen here, since every dropped rule matched nothing and
// its live replacement is added by the same merge.
func stripRenamedWriteRules(existing, generated []any) []any {
	twins := make(map[string]struct{}, len(generated))
	for _, g := range generated {
		s, ok := g.(string)
		if !ok {
			continue
		}
		if path, found := strings.CutPrefix(s, "Edit("); found && strings.HasSuffix(path, ")") {
			twins["Write("+path] = struct{}{}
		}
	}
	if len(twins) == 0 {
		return existing
	}
	out := make([]any, 0, len(existing))
	for _, e := range existing {
		if s, ok := e.(string); ok {
			if _, renamed := twins[s]; renamed {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// MergeSettings deep-merges generated into existing, preserving the user's
// entries. Permission/mcp arrays are unioned (existing first, generated extras
// appended), minus tu-agent's own rules that a rename made inert; hook lists are
// unioned by matcher; scalars tu-agent owns are set only when absent; unknown
// user keys are left untouched. Pure: no I/O.
func MergeSettings(existing, generated map[string]any) map[string]any {
	out := cloneMap(existing)

	if gp, ok := generated["permissions"].(map[string]any); ok {
		ep, _ := out["permissions"].(map[string]any)
		if ep == nil {
			ep = map[string]any{}
		}
		for _, k := range []string{"allow", "deny", "ask"} {
			if gv, ok := gp[k].([]any); ok {
				ep[k] = unionAny(stripRenamedWriteRules(asAnySlice(ep[k]), gv), gv)
			}
		}
		if _, ok := ep["defaultMode"]; !ok {
			if dv, ok := gp["defaultMode"]; ok {
				ep["defaultMode"] = dv
			}
		}
		out["permissions"] = ep
	}

	if gh, ok := generated["hooks"].(map[string]any); ok {
		eh, _ := out["hooks"].(map[string]any)
		if eh == nil {
			eh = map[string]any{}
		}
		// Visit the UNION of event keys (existing ∪ generated), not just the
		// generated set. hardenHooks(pluginPresent=true) omits SessionStart/
		// Stop/SessionEnd entirely (the plugin's hooks.json owns those), so an
		// existing settings.json hardened STANDALONE still has tu-agent-managed
		// entries under those keys that generated never re-visits. Skipping
		// them here would leave stale hooks in place, double-firing alongside
		// the plugin's own hooks on every upgrade.
		events := make(map[string]struct{}, len(eh)+len(gh))
		for event := range eh {
			events[event] = struct{}{}
		}
		for event := range gh {
			events[event] = struct{}{}
		}
		for event := range events {
			merged := unionHookEntries(asAnySlice(eh[event]), asAnySlice(gh[event]))
			if len(merged) == 0 {
				// Stripping tu-agent-managed entries left nothing behind (no user
				// entries, no fresh generated ones): drop the key entirely rather
				// than leave an empty "Event": [] husk in settings.json.
				delete(eh, event)
				continue
			}
			eh[event] = merged
		}
		out["hooks"] = eh
	}

	if gv, ok := generated["enabledMcpjsonServers"].([]any); ok {
		out["enabledMcpjsonServers"] = unionAny(asAnySlice(out["enabledMcpjsonServers"]), gv)
	}

	for _, k := range []string{"includeCoAuthoredBy", "cleanupPeriodDays"} {
		if _, ok := out[k]; !ok {
			if gv, ok := generated[k]; ok {
				out[k] = gv
			}
		}
	}
	return out
}

// cloneMap deep-clones via a JSON round-trip, normalizing all values to the
// types encoding/json produces so merges and comparisons are consistent.
// Precondition: m must be JSON-round-trippable (no chan/func/complex values);
// callers pass maps that came from json.Unmarshal, so this always holds.
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

// asAnySlice returns v as []any, or nil if it is not a slice.
func asAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// unionAny appends generated string entries not already present in existing,
// preserving existing order. Non-string entries in generated are appended as-is.
func unionAny(existing, generated []any) []any {
	seen := map[string]bool{}
	out := make([]any, 0, len(existing)+len(generated))
	for _, v := range existing {
		out = append(out, v)
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}
	for _, v := range generated {
		if s, ok := v.(string); ok {
			if seen[s] {
				continue
			}
			seen[s] = true
		}
		out = append(out, v)
	}
	return out
}

// hookEntryKey returns a string key for a hook entry used to detect duplicates.
// Entries with a "matcher" field are keyed by that matcher. Entries without one
// (e.g. SessionStart entries) are keyed by their JSON representation so that
// identical matcher-less entries are not appended more than once.
func hookEntryKey(e any) string {
	if m, ok := e.(map[string]any); ok {
		if mt, ok := m["matcher"].(string); ok {
			return "matcher:" + mt
		}
	}
	b, _ := json.Marshal(e)
	return "json:" + string(b)
}

// isTuAgentManagedHook reports whether a hook entry was generated by
// hardenHooks (current or legacy), so re-hardening replaces it instead of
// leaving a stale duplicate. It matches only the hooks that hardenHooks itself
// owns and re-generates:
//   - the current secret-guard entry (command contains "tu-agent guard-path")
//   - the legacy jq secret-guard (command contains the refusal text)
//   - the memory import/export/materialize/crystallize hooks (commands contain
//     "tu-agent memory import", "tu-agent memory export", "tu-agent memory
//     materialize", or "tu-agent memory crystallize")
//
// Hooks installed by other tu-agent subcommands (e.g. "tu-agent graph update"
// from "tu-agent setup --hooks") and language formatters are NOT matched, so
// they are preserved across re-hardening.
func isTuAgentManagedHook(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, "tu-agent guard-path") ||
			strings.Contains(cmd, "refusing to modify secret/credential files") ||
			strings.Contains(cmd, "tu-agent memory import") ||
			strings.Contains(cmd, "tu-agent memory export") ||
			strings.Contains(cmd, "tu-agent memory relink") ||
			strings.Contains(cmd, "tu-agent memory materialize") ||
			strings.Contains(cmd, "tu-agent memory crystallize") {
			return true
		}
	}
	return false
}

// unionHookEntries appends generated hook entries not already present in
// existing, preserving existing entries. Entries with a "matcher" field are
// deduplicated by matcher; entries without one (e.g. SessionStart) are
// deduplicated by their full JSON representation.
//
// tu-agent-managed entries in existing (current or legacy) are dropped and
// replaced by the freshly generated ones — this ensures re-hardening migrates
// stale hooks instead of accumulating duplicates.
func unionHookEntries(existing, generated []any) []any {
	seen := map[string]bool{}
	out := make([]any, 0, len(existing)+len(generated))
	for _, e := range existing {
		if isTuAgentManagedHook(e) {
			continue // replaced by the freshly generated entry below
		}
		out = append(out, e)
		seen[hookEntryKey(e)] = true
	}
	for _, e := range generated {
		if k := hookEntryKey(e); !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}
