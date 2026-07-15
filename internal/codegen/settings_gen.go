package codegen

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

// hardenDeny lists hard-blocked operations: destructive shell, irreversible git
// (reset --hard, clean -fd), privilege escalation, pipe-to-shell, and
// reading/writing secrets and credentials. Read(...) rules gate the Read tool;
// Edit(...) rules gate every file-editing tool.
func hardenDeny() []any {
	return strSlice(
		"Bash(rm -rf *)",
		"Bash(git push --force*)",
		"Bash(git push -f *)",
		"Bash(git push -f*)",
		"Bash(git reset --hard*)",
		"Bash(git clean -fd*)",
		"Bash(curl * | bash*)",
		"Bash(curl *| bash*)",
		"Bash(curl *|bash*)",
		"Bash(wget * | sh*)",
		"Bash(wget *| sh*)",
		"Bash(wget *|sh*)",
		"Bash(sudo *)",
		"Read(./.env)", "Read(./.env.*)", "Read(**/*.pem)", "Read(**/*.key)",
		"Read(**/id_rsa*)", "Read(**/id_ed25519*)", "Read(**/.ssh/**)", "Read(**/.aws/**)", "Read(./secrets/**)",
		// Edit(...), not Write(...): Claude Code matches file-permission rules
		// only against Edit(path), which covers every file-editing tool (Write,
		// Edit, NotebookEdit). A Write(path) rule matches nothing and the
		// harness reports it as inert at startup.
		"Edit(./.env)", "Edit(./.env.*)", "Edit(**/*.pem)", "Edit(**/*.key)",
		"Edit(**/id_rsa*)", "Edit(**/id_ed25519*)", "Edit(**/.ssh/**)", "Edit(**/.aws/**)", "Edit(./secrets/**)",
		// Belt-and-suspenders only: these catch `cat` of common secrets. The robust
		// layer is the Write|Edit|Bash guard hook (CommandTouchesSecret), which
		// covers any reader (less/head/base64/cp) of any secret path.
		"Bash(cat *id_rsa*)", "Bash(cat *id_ed25519*)", "Bash(cat *.ssh/*)", "Bash(cat *.pem)", "Bash(cat *.key)", "Bash(cat *.env*)",
	)
}

// hardenAsk lists operations allowed only with human confirmation: write-side
// git (the harness never commits on its own — product rule), other
// irreversible or outward-facing actions, and WebFetch (research works,
// exfiltration gated).
func hardenAsk() []any {
	return strSlice(
		"Bash(git add *)", "Bash(git commit *)", "Bash(git branch *)", "Bash(git checkout *)",
		"Bash(git push *)", "Bash(gh pr *)", "Bash(gh release *)",
		"Bash(docker push *)", "Bash(kubectl *)", "Bash(terraform apply*)",
		"WebFetch",
	)
}

// hardenAllow lists pre-approved operations: read-only git only (write-side git
// requires human confirmation via hardenAsk — the harness never commits on its
// own), WebSearch, and the detected toolchain's build/test/lint commands.
func hardenAllow(lang, buildTool string) []any {
	base := []string{
		"Bash(git status*)", "Bash(git diff*)", "Bash(git log*)",
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
		// .claude/skills files, and surface deterministic advise suggestions (advise's
		// crystallize-ready rule absorbs the former standalone crystallize nudge). All
		// are incremental/idempotent and degrade to a no-op without the binary. One entry,
		// multiple commands — keeps the single tu-agent-managed SessionStart entry the
		// migration logic expects.
		hooks["SessionStart"] = []any{
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent graph update --quiet --announce"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory import --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory relink --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent memory materialize --quiet"},
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent advise --nudge"},
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
		// Behavioral friction signal (F5-A): record one row per user prompt.
		// Standalone parity with the plugin's UserPromptSubmit hook.
		hooks["UserPromptSubmit"] = []any{
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": binaryGuardClause + "tu-agent hook prompt-submit"},
				},
			},
		}
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
