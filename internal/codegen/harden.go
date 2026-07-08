package codegen

import (
	"encoding/json"
	"regexp"
	"strings"
)

// secretPathPattern matches file paths the secret-guard refuses to read or
// modify (the Write/Edit tool path and the Read/Write deny rules).
const secretPathPattern = `(^|/)(\.env($|\.)|secrets/|\.ssh/|\.aws/|\.gnupg/|\.config/gcloud/|id_rsa|id_ed25519|id_ecdsa)|\.(pem|key)$`

// secretPathRe is secretPathPattern compiled once for IsSecretPath.
var secretPathRe = regexp.MustCompile(secretPathPattern)

// IsSecretPath reports whether a single file path is a secret/credential file.
func IsSecretPath(p string) bool {
	return secretPathRe.MatchString(p)
}

// secretCommandPattern matches a secret/credential path mentioned anywhere in a
// Bash command (block-on-mention). Directory and key-name tokens match at a
// shell-ish boundary (start, space, /, ~, quote, =); .env requires a
// trailing path boundary so a commit message mentioning ".env" does not trip it.
const secretCommandPattern = `(^|[\s/~"'=])(\.ssh/|\.aws/|\.gnupg/|\.config/gcloud/|secrets/|id_rsa|id_ed25519|id_ecdsa|\.env($|[./]))`

// secretCommandExtPattern matches .pem and .key file extensions with a trailing
// path boundary (end-of-string, whitespace, or quote/slash) but no leading
// boundary requirement, so extensions on arbitrary filenames are caught.
const secretCommandExtPattern = `\.(pem|key)($|[\s"'/])`

// secretCommandRe and secretCommandExtRe are compiled once for CommandTouchesSecret.
var (
	secretCommandRe    = regexp.MustCompile(secretCommandPattern)
	secretCommandExtRe = regexp.MustCompile(secretCommandExtPattern)
)

// CommandTouchesSecret reports whether a Bash command names a secret/credential
// path. It is a string scan, not a shell parser: deny-wins on mention.
func CommandTouchesSecret(command string) bool {
	return secretCommandRe.MatchString(command) || secretCommandExtRe.MatchString(command)
}

// sensitiveEnvMarker matches the secret-bearing segment of an environment
// variable name (API_KEY, TOKEN, SECRET, …). Kept as a fragment so it can be
// reused inside the expansion, printenv, and dump patterns below.
const sensitiveEnvMarker = `(?:API_?KEY|APIKEY|SECRET|TOKEN|PASSWORD|PASSWD|PASSPHRASE|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)`

var (
	// printBuiltinRe matches echo/printf appearing as a command word (start, or
	// after a separator), i.e. a command that writes its arguments to stdout.
	printBuiltinRe = regexp.MustCompile("(?i)(^|[\\s;&|(`])(echo|printf)([\\s]|$)")
	// envSecretExpansionRe matches a $VAR / ${VAR…} expansion whose name carries a
	// secret marker — the value that would reach stdout if printed.
	envSecretExpansionRe = regexp.MustCompile(`(?i)\$\{?[A-Z0-9_]*` + sensitiveEnvMarker)
	// printenvSecretRe matches `printenv <SECRET_NAME>`, a direct value print.
	printenvSecretRe = regexp.MustCompile("(?i)(^|[\\s;&|(`])printenv\\s+[A-Z0-9_]*" + sensitiveEnvMarker)
	// envDumpRe matches a bare `env`/`printenv` that dumps EVERY variable (no
	// run-target follows): end-of-command, a pipe, a redirect, or a separator.
	// `env FOO=bar cmd` / `env cmd` (a run-target follows) do not match.
	envDumpRe = regexp.MustCompile("(?i)(^|[;&|(`])[ \\t]*(env|printenv)[ \\t]*($|[|>;&\\n])")
)

// CommandExposesEnvSecret reports whether a Bash command could print a secret
// environment variable's VALUE to stdout — the exposure class CommandTouchesSecret
// (secret FILES) does not cover. Deny-wins on the pattern, not a shell parse.
// It blocks: a dump of the whole environment (`env`, `printenv`, `env | …`);
// `printenv <SECRET_NAME>`; and echo/printf combined with a $-expansion of a
// secret-named variable. It deliberately does NOT block merely USING a secret
// (e.g. passing $API_KEY to curl), only printing it.
func CommandExposesEnvSecret(command string) bool {
	if envDumpRe.MatchString(command) || printenvSecretRe.MatchString(command) {
		return true
	}
	return envSecretExpansionRe.MatchString(command) && printBuiltinRe.MatchString(command)
}

// HardenedSettings returns the tu-agent-owned Claude Code settings.json content
// as a generic map, scoped to the detected language and build tool. pluginPresent
// signals that the Claude Code plugin's own hooks.json already supplies the
// graph/memory hooks, so hardenHooks omits the duplicates. Pure: no I/O.
// Types mirror encoding/json output ([]any, float64, ...) so the result merges
// cleanly with a settings file round-tripped through json.Unmarshal.
func HardenedSettings(lang, buildTool string, pluginPresent bool) map[string]any {
	return map[string]any{
		"permissions": map[string]any{
			"defaultMode": "default",
			"deny":        hardenDeny(),
			"ask":         hardenAsk(),
			"allow":       hardenAllow(lang, buildTool),
		},
		"hooks":                 hardenHooks(lang, pluginPresent),
		"enabledMcpjsonServers": []any{"tu-agent-graph"},
		"includeCoAuthoredBy":   true,
		"cleanupPeriodDays":     float64(30),
	}
}

func strSlice(ss ...string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// hardenDeny lists hard-blocked operations: destructive shell, privilege
// escalation, pipe-to-shell, and reading/writing secrets and credentials.
func hardenDeny() []any {
	return strSlice(
		"Bash(rm -rf *)",
		"Bash(git push --force*)",
		"Bash(git push -f *)",
		"Bash(git push -f*)",
		"Bash(curl * | bash*)",
		"Bash(curl *| bash*)",
		"Bash(curl *|bash*)",
		"Bash(wget * | sh*)",
		"Bash(wget *| sh*)",
		"Bash(wget *|sh*)",
		"Bash(sudo *)",
		"Read(./.env)", "Read(./.env.*)", "Read(**/*.pem)", "Read(**/*.key)",
		"Read(**/id_rsa*)", "Read(**/id_ed25519*)", "Read(**/.ssh/**)", "Read(**/.aws/**)", "Read(./secrets/**)",
		"Write(./.env)", "Write(./.env.*)", "Write(**/*.pem)", "Write(**/*.key)",
		"Write(**/id_rsa*)", "Write(**/id_ed25519*)", "Write(**/.ssh/**)", "Write(**/.aws/**)", "Write(./secrets/**)",
		// Belt-and-suspenders only: these catch `cat` of common secrets. The robust
		// layer is the Write|Edit|Bash guard hook (CommandTouchesSecret), which
		// covers any reader (less/head/base64/cp) of any secret path.
		"Bash(cat *id_rsa*)", "Bash(cat *id_ed25519*)", "Bash(cat *.ssh/*)", "Bash(cat *.pem)", "Bash(cat *.key)", "Bash(cat *.env*)",
	)
}

// hardenAsk lists operations allowed only with human confirmation: irreversible
// or outward-facing actions, and WebFetch (research works, exfiltration gated).
func hardenAsk() []any {
	return strSlice(
		"Bash(git push *)", "Bash(gh pr *)", "Bash(gh release *)",
		"Bash(docker push *)", "Bash(kubectl *)", "Bash(terraform apply*)",
		"WebFetch",
	)
}

// hardenAllow lists pre-approved operations: read-only git, safe write-side git,
// WebSearch, and the detected toolchain's build/test/lint commands.
func hardenAllow(lang, buildTool string) []any {
	base := []string{
		"Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
		"Bash(git add *)", "Bash(git commit *)", "Bash(git branch *)", "Bash(git checkout *)",
		"WebSearch",
	}
	base = append(base, toolchainAllow(lang, buildTool)...)
	return strSlice(base...)
}

// toolchainAllow returns the build/test/lint commands to pre-approve for a
// language and build tool. Returns nil for unknown toolchains.
func toolchainAllow(lang, buildTool string) []string {
	switch lang {
	case "go":
		return []string{
			"Bash(go build*)", "Bash(go test*)", "Bash(go vet*)", "Bash(go fmt*)",
			"Bash(go run*)", "Bash(go mod *)", "Bash(gofmt *)", "Bash(golangci-lint run*)",
		}
	case "java":
		switch buildTool {
		case "gradle":
			return []string{"Bash(./gradlew *)", "Bash(gradle *)"}
		case "maven":
			return []string{"Bash(mvn *)", "Bash(./mvnw *)"}
		}
		return nil
	case "python":
		return []string{"Bash(pytest*)", "Bash(python -m *)", "Bash(pip install*)", "Bash(ruff *)", "Bash(black *)"}
	case "typescript":
		return []string{"Bash(npm run *)", "Bash(npm test*)", "Bash(npx *)", "Bash(yarn *)", "Bash(pnpm *)"}
	}
	return nil
}

// hardenHooks builds the PreToolUse secret guard (always), a PostToolUse
// formatter (when the language has a nameable formatter), and — unless
// pluginPresent — a SessionStart refresh (graph update + memory import) and
// Stop/SessionEnd memory export.
func hardenHooks(lang string, pluginPresent bool) map[string]any {
	hooks := map[string]any{
		"PreToolUse": []any{
			map[string]any{
				"matcher": "Write|Edit|Bash|Read",
				"hooks":   []any{map[string]any{"type": "command", "command": secretGuardCommand()}},
			},
		},
	}
	if post := postFormatCommand(lang); post != "" {
		hooks["PostToolUse"] = []any{
			map[string]any{
				"matcher": "Write|Edit",
				"hooks":   []any{map[string]any{"type": "command", "command": post}},
			},
		}
	}
	// the plugin's hooks.json already provides these; registering them again
	// would run every export twice per event.
	if !pluginPresent {
		// On session start, refresh the graph (so structural queries are not stale
		// after an external `git pull` that the PostToolUse edit hook never saw),
		// import teammates' shared memory, materialize crystallized skills to local
		// .claude/skills files, and surface the crystallize nudge. All are
		// incremental/idempotent and degrade to a no-op without the binary. One entry,
		// multiple commands — keeps the single tu-agent-managed SessionStart entry the
		// migration logic expects.
		hooks["SessionStart"] = []any{
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent graph update --quiet --announce"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory import --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory relink --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory materialize --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory crystallize --nudge"},
				},
			},
		}
		// Auto-export captured memory to the author's chunk on every turn end (Stop)
		// and at session end (SessionEnd). Export is idempotent (skip-if-unchanged),
		// so running it on both events causes no git churn. Publishing the chunk
		// (git commit/push) stays manual.
		autoExport := []any{
			map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory export --quiet"}},
			},
		}
		hooks["Stop"] = autoExport
		hooks["SessionEnd"] = autoExport
	}
	return hooks
}

// binaryGuardClause makes every generated hook a no-op when the tu-agent binary
// is not on PATH (a teammate without it gets no errors). It is a guard CLAUSE,
// not `&& cmd || exit 0`: the latter would swallow a blocking hook's exit 2 into
// exit 0. With this form the following command's own exit code propagates.
const binaryGuardClause = `command -v tu-agent >/dev/null 2>&1 || exit 0; `

// secretGuardCommand returns the PreToolUse hook: when the binary is present it
// execs `tu-agent guard-path`, which blocks (exit 2) writes to secret files.
func secretGuardCommand() string {
	return binaryGuardClause + "exec tu-agent guard-path"
}

// postFormatCommand returns a non-failing formatter command for the language,
// or "" when none applies (e.g. Java has no universal lightweight formatter).
func postFormatCommand(lang string) string {
	switch lang {
	case "go":
		return `command -v gofmt >/dev/null 2>&1 && gofmt -w . ; true`
	case "python":
		return `command -v ruff >/dev/null 2>&1 && ruff format . >/dev/null 2>&1 ; true`
	case "typescript":
		return `command -v npx >/dev/null 2>&1 && npx --no-install prettier -w . >/dev/null 2>&1 ; true`
	}
	return ""
}

// MergeSettings deep-merges generated into existing, preserving the user's
// entries. Permission/mcp arrays are unioned (existing first, generated extras
// appended); hook lists are unioned by matcher; scalars tu-agent owns are set
// only when absent; unknown user keys are left untouched. Pure: no I/O.
func MergeSettings(existing, generated map[string]any) map[string]any {
	out := cloneMap(existing)

	if gp, ok := generated["permissions"].(map[string]any); ok {
		ep, _ := out["permissions"].(map[string]any)
		if ep == nil {
			ep = map[string]any{}
		}
		for _, k := range []string{"allow", "deny", "ask"} {
			if gv, ok := gp[k].([]any); ok {
				ep[k] = unionAny(asAnySlice(ep[k]), gv)
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

const (
	gitignoreOpen  = "# >>> tu-agent >>>"
	gitignoreClose = "# <<< tu-agent <<<"

	gitExcludeOpen  = "# >>> tu-agent (private) >>>"
	gitExcludeClose = "# <<< tu-agent (private) <<<"
)

// GitInfoExcludeBlock returns the managed block for .git/info/exclude used by
// private mode: it keeps ALL tu-agent / Claude Code artifacts out of commits
// without naming them in a committed file. .git/info/exclude is local per clone
// and never committed, so the ignore rules themselves leave no trace in history.
func GitInfoExcludeBlock() string {
	return gitExcludeOpen + "\n" +
		"# Local-only excludes (this file is never committed). Same syntax as .gitignore.\n" +
		".claude/\n" +
		"CLAUDE.md\n" +
		".mcp.json\n" +
		"AGENTS.md\n" +
		"# Everything under .tu-agent stays local EXCEPT the shared-memory chunks,\n" +
		"# re-included (step by step, since git won't re-include under an excluded dir)\n" +
		"# so a team can still commit them. graph.db / memory.db / telemetry stay local.\n" +
		".tu-agent/*\n" +
		"!.tu-agent/memory/\n" +
		".tu-agent/memory/*\n" +
		"!.tu-agent/memory/chunks/\n" +
		gitExcludeClose
}

// MergeGitInfoExclude upserts the private managed block into existing
// .git/info/exclude content. Idempotent. Pure: no I/O.
func MergeGitInfoExclude(existing string) string {
	return mergeManagedBlock(existing, GitInfoExcludeBlock(), gitExcludeOpen, gitExcludeClose)
}

// GitignoreBlock returns the managed block listing tu-agent's derived artifacts.
func GitignoreBlock() string {
	return gitignoreOpen + "\n" +
		".tu-agent/graph.db\n" +
		".tu-agent/graph.db-wal\n" +
		".tu-agent/graph.db-shm\n" +
		".tu-agent/telemetry.jsonl\n" +
		".tu-agent/memory.db\n" +
		".tu-agent/memory.db-wal\n" +
		".tu-agent/memory.db-shm\n" +
		".claude/settings.json.bak\n" +
		"# .tu-agent/memory/chunks/ is intentionally versioned (shared team memory)\n" +
		gitignoreClose
}

// MergeGitignore upserts the tu-agent managed block into existing .gitignore
// content: replaced in place if the markers are present, appended otherwise.
// Idempotent. Pure: no I/O.
func MergeGitignore(existing string) string {
	return mergeManagedBlock(existing, GitignoreBlock(), gitignoreOpen, gitignoreClose)
}

// mergeManagedBlock upserts a marker-delimited block into existing content:
// replaced in place if the markers are present, appended otherwise. Idempotent.
// Pure: no I/O. Shared by MergeGitignore and MergeGitInfoExclude.
func mergeManagedBlock(existing, block, open, close string) string {
	start := strings.Index(existing, open)
	endMarker := strings.Index(existing, close)
	if start >= 0 && endMarker > start {
		end := endMarker + len(close)
		result := existing[:start] + block + existing[end:]
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result
	}
	sep := ""
	if existing != "" {
		if !strings.HasSuffix(existing, "\n") {
			sep = "\n"
		}
		sep += "\n"
	}
	return existing + sep + block + "\n"
}
