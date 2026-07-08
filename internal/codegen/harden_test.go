package codegen

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// permList returns permissions[key] as a []string for assertions.
func permList(t *testing.T, s map[string]any, key string) []string {
	t.Helper()
	perms, ok := s["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type")
	}
	raw, ok := perms[key].([]any)
	if !ok {
		t.Fatalf("permissions.%s missing or not []any", key)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestHardenedSettings_PermissionsAndScalars(t *testing.T) {
	s := HardenedSettings("go", "go", false)

	deny := permList(t, s, "deny")
	for _, want := range []string{
		"Read(./.env)", "Read(**/*.pem)", "Read(**/id_rsa*)", "Read(./secrets/**)",
		"Write(./.env)", "Bash(rm -rf *)", "Bash(sudo *)", "Bash(git push --force*)",
	} {
		if !contains(deny, want) {
			t.Errorf("deny missing %q", want)
		}
	}

	ask := permList(t, s, "ask")
	if !contains(ask, "WebFetch") {
		t.Error("ask missing WebFetch")
	}
	if !contains(ask, "Bash(git push *)") {
		t.Error("ask missing git push")
	}

	allow := permList(t, s, "allow")
	if !contains(allow, "WebSearch") {
		t.Error("allow missing WebSearch")
	}
	if contains(deny, "WebFetch") || contains(allow, "WebFetch") {
		t.Error("WebFetch must be ask-only, not in deny/allow")
	}

	perms := s["permissions"].(map[string]any)
	if perms["defaultMode"] != "default" {
		t.Errorf("defaultMode = %v, want default", perms["defaultMode"])
	}
	mcp, ok := s["enabledMcpjsonServers"].([]any)
	if !ok || len(mcp) != 1 || mcp[0] != "tu-agent-graph" {
		t.Errorf("enabledMcpjsonServers = %v, want [tu-agent-graph]", s["enabledMcpjsonServers"])
	}
	if s["includeCoAuthoredBy"] != true {
		t.Error("includeCoAuthoredBy should be true")
	}
	if s["cleanupPeriodDays"] != float64(30) {
		t.Errorf("cleanupPeriodDays = %v, want 30", s["cleanupPeriodDays"])
	}
	if _, ok := s["env"]; ok {
		t.Error("generic generator must not emit env")
	}
}

func TestHardenedSettings_Toolchains(t *testing.T) {
	cases := []struct {
		lang, build string
		wantAllow   string
		wantPost    bool
	}{
		{"go", "go", "Bash(go test*)", true},
		{"java", "gradle", "Bash(./gradlew *)", false},
		{"java", "maven", "Bash(mvn *)", false},
		{"python", "pyproject", "Bash(pytest*)", true},
		{"typescript", "npm", "Bash(npm run *)", true},
	}
	for _, c := range cases {
		t.Run(c.lang+"/"+c.build, func(t *testing.T) {
			s := HardenedSettings(c.lang, c.build, false)
			if !contains(permList(t, s, "allow"), c.wantAllow) {
				t.Errorf("allow missing %q", c.wantAllow)
			}
			hooks := s["hooks"].(map[string]any)
			if _, ok := hooks["PreToolUse"]; !ok {
				t.Error("PreToolUse secret guard missing")
			}
			_, hasPost := hooks["PostToolUse"]
			if hasPost != c.wantPost {
				t.Errorf("PostToolUse present=%v, want %v", hasPost, c.wantPost)
			}
		})
	}
}

func TestHardenedSettingsSessionStartImport(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings has no hooks map")
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok || len(ss) == 0 {
		t.Fatal("no SessionStart hook")
	}
	// SessionStart refreshes BOTH the graph (so structural queries after an
	// external `git pull` are not stale) and shared memory. Collect every command
	// across the entry's inner hooks rather than assuming an order.
	var cmds []string
	for _, e := range ss {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if c, ok := hm["command"].(string); ok {
					cmds = append(cmds, c)
				}
			}
		}
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "graph update") || !strings.Contains(joined, "--quiet") {
		t.Errorf("SessionStart should refresh the graph (graph update --quiet); got %q", joined)
	}
	if !strings.Contains(joined, "tu-agent graph update --quiet --announce") {
		t.Errorf("SessionStart graph update must carry --announce (nudge for the agent's context); got %q", joined)
	}
	if !strings.Contains(joined, "memory import") {
		t.Errorf("SessionStart should import shared memory (memory import); got %q", joined)
	}
	if !strings.Contains(joined, "memory relink") {
		t.Errorf("SessionStart should run memory relink --quiet; got %q", joined)
	}
}

func TestHardenSessionStartIncludesRelink(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings has no hooks map")
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok || len(ss) == 0 {
		t.Fatal("no SessionStart hook")
	}
	var cmds []string
	for _, e := range ss {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if c, ok := hm["command"].(string); ok {
					cmds = append(cmds, c)
				}
			}
		}
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "tu-agent memory relink --quiet") {
		t.Errorf("SessionStart must include memory relink, got:\n%s", joined)
	}
}

func TestHardenedSettingsAutoExportHooks(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings has no hooks map")
	}
	for _, ev := range []string{"Stop", "SessionEnd"} {
		entries, ok := hooks[ev].([]any)
		if !ok || len(entries) == 0 {
			t.Fatalf("no %s hook", ev)
		}
		entry := entries[0].(map[string]any)
		inner := entry["hooks"].([]any)
		cmd := inner[0].(map[string]any)["command"].(string)
		if !strings.Contains(cmd, "memory export") || !strings.Contains(cmd, "--quiet") {
			t.Fatalf("%s hook should run memory export --quiet, got %q", ev, cmd)
		}
	}
}

func TestHardenedSettings_IsValidJSON(t *testing.T) {
	b, err := json.Marshal(HardenedSettings("go", "go", false))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "tu-agent-graph") {
		t.Error("expected tu-agent-graph in output")
	}
}

func TestSecretPathPattern(t *testing.T) {
	re := regexp.MustCompile(secretPathPattern)
	blocked := []string{".env", ".env.local", "config/secrets/db.yml", "id_rsa", "deploy/id_rsa.pub", "certs/server.pem"}
	for _, p := range blocked {
		if !re.MatchString(p) {
			t.Errorf("expected %q to be blocked", p)
		}
	}
	allowed := []string{"main.go", "README.md", "internal/environment.go", "src/preventing.ts"}
	for _, p := range allowed {
		if re.MatchString(p) {
			t.Errorf("expected %q to be allowed", p)
		}
	}
}

func TestMergeSettings_AddsIntoEmpty(t *testing.T) {
	gen := HardenedSettings("go", "go", false)
	merged := MergeSettings(map[string]any{}, gen)
	if !contains(permList(t, merged, "deny"), "Bash(rm -rf *)") {
		t.Error("merge into empty lost deny entries")
	}
}

func TestMergeSettings_Idempotent(t *testing.T) {
	gen := HardenedSettings("go", "go", false)
	once := MergeSettings(map[string]any{}, gen)
	twice := MergeSettings(once, gen)
	b1, _ := json.Marshal(once)
	b2, _ := json.Marshal(twice)
	if string(b1) != string(b2) {
		t.Errorf("merge not idempotent:\n once=%s\n twice=%s", b1, b2)
	}
}

func TestMergeSettings_PreservesUserEntries(t *testing.T) {
	existing := map[string]any{
		"permissions": map[string]any{
			"defaultMode": "acceptEdits",
			"allow":       []any{"Bash(my-custom-tool*)"},
		},
		"includeCoAuthoredBy": false,
		"customUserKey":       "keep-me",
	}
	merged := MergeSettings(existing, HardenedSettings("go", "go", false))

	perms := merged["permissions"].(map[string]any)
	if perms["defaultMode"] != "acceptEdits" {
		t.Errorf("user defaultMode overwritten: %v", perms["defaultMode"])
	}
	allow := permList(t, merged, "allow")
	if !contains(allow, "Bash(my-custom-tool*)") {
		t.Error("dropped user allow entry")
	}
	if !contains(allow, "WebSearch") {
		t.Error("did not add tu-agent allow entry alongside user's")
	}
	if merged["includeCoAuthoredBy"] != false {
		t.Error("overwrote user includeCoAuthoredBy")
	}
	if merged["customUserKey"] != "keep-me" {
		t.Error("dropped unknown user key")
	}
}

func TestMergeSettings_UnionsHooksByMatcher(t *testing.T) {
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "echo user"}}},
			},
		},
	}
	merged := MergeSettings(existing, HardenedSettings("go", "go", false))
	pre := merged["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("PreToolUse len = %d, want 2", len(pre))
	}
}

func TestMergeGitignore_AddsBlockOnce(t *testing.T) {
	out := MergeGitignore("")
	if !strings.Contains(out, ".tu-agent/graph.db") {
		t.Error("missing graph.db entry")
	}
	if !strings.Contains(out, "# >>> tu-agent >>>") {
		t.Error("missing managed-block marker")
	}
	twice := MergeGitignore(out)
	if strings.Count(twice, "# >>> tu-agent >>>") != 1 {
		t.Errorf("block duplicated: %d markers", strings.Count(twice, "# >>> tu-agent >>>"))
	}
	if twice != out {
		t.Error("MergeGitignore not idempotent")
	}
}

func TestMergeGitignore_PreservesExisting(t *testing.T) {
	out := MergeGitignore("node_modules/\n*.log\n")
	if !strings.Contains(out, "node_modules/") || !strings.Contains(out, "*.log") {
		t.Error("dropped existing .gitignore lines")
	}
	if !strings.Contains(out, ".tu-agent/graph.db") {
		t.Error("did not append tu-agent block")
	}
}

func TestMergeGitignore_ReplaceInPlacePreservesSurrounding(t *testing.T) {
	first := MergeGitignore("node_modules/\n")
	withAfter := first + "dist/\n"
	out := MergeGitignore(withAfter)
	if !strings.Contains(out, "node_modules/") || !strings.Contains(out, "dist/") {
		t.Error("replace-in-place dropped surrounding lines")
	}
	if strings.Count(out, "# >>> tu-agent >>>") != 1 {
		t.Errorf("block not replaced in place: %d markers", strings.Count(out, "# >>> tu-agent >>>"))
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("result should end with newline")
	}
	if MergeGitignore(out) != out {
		t.Error("replace-in-place not idempotent")
	}
}

func TestGitInfoExcludeBlock_SharesMemoryChunks(t *testing.T) {
	b := GitInfoExcludeBlock()
	// The shared-memory chunks dir is re-included so a team can still commit it
	// even in private mode (everything else tu-agent stays local).
	if !strings.Contains(b, "!.tu-agent/memory/chunks/") {
		t.Error("private exclude must re-include .tu-agent/memory/chunks/ so memory can be shared")
	}
	// The rest of .tu-agent (graph.db, memory.db, telemetry) must still be ignored
	// via a contents glob, not a wholesale dir exclude (which would block re-include).
	if !strings.Contains(b, ".tu-agent/*") {
		t.Error("private exclude must ignore the rest of .tu-agent via .tu-agent/*")
	}
	if strings.Contains(b, "\n.tu-agent/\n") {
		t.Error("a wholesale .tu-agent/ exclude blocks re-including the chunks dir")
	}
}

func TestGitInfoExcludeBlock_CoversArtifacts(t *testing.T) {
	b := GitInfoExcludeBlock()
	for _, want := range []string{".claude/", "CLAUDE.md", ".mcp.json", ".tu-agent/", "AGENTS.md"} {
		if !strings.Contains(b, want) {
			t.Errorf("private exclude block missing %q", want)
		}
	}
	if !strings.Contains(b, "# >>> tu-agent (private) >>>") {
		t.Error("missing private managed-block marker")
	}
}

func TestMergeGitInfoExclude_AddsBlockOnce(t *testing.T) {
	out := MergeGitInfoExclude("")
	if !strings.Contains(out, ".claude/") {
		t.Error("missing .claude/ entry")
	}
	twice := MergeGitInfoExclude(out)
	if strings.Count(twice, "# >>> tu-agent (private) >>>") != 1 {
		t.Errorf("block duplicated: %d markers", strings.Count(twice, "# >>> tu-agent (private) >>>"))
	}
	if twice != out {
		t.Error("MergeGitInfoExclude not idempotent")
	}
}

func TestMergeGitInfoExclude_PreservesExisting(t *testing.T) {
	out := MergeGitInfoExclude("# git ships this comment\nbuild-local/\n")
	if !strings.Contains(out, "build-local/") {
		t.Error("dropped existing exclude lines")
	}
	if !strings.Contains(out, ".tu-agent/") {
		t.Error("did not append private block")
	}
}

func TestIsSecretPath(t *testing.T) {
	for _, p := range []string{
		"./.env", ".env.local", "core/x.pem", "config/server.key",
		"/home/u/.ssh/id_rsa", "/home/u/.ssh/id_ed25519", "/home/u/.aws/credentials",
		"/home/u/.config/gcloud/access_tokens.db", "secrets/db.txt",
	} {
		if !IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{
		"main.go", "README.md", "internal/env/config.go", "docs/keynote.md",
	} {
		if IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = true, want false", p)
		}
	}
}

func TestCommandTouchesSecret(t *testing.T) {
	for _, c := range []string{
		"cat ~/.ssh/id_rsa", "ls -la ~/.ssh/", "cat ~/.ssh/id_ed25519.pub",
		"base64 .env", "cp ~/.aws/credentials /tmp", "cat ./secrets/db.txt",
		"head config/tls.pem", "cat server.key",
	} {
		if !CommandTouchesSecret(c) {
			t.Errorf("CommandTouchesSecret(%q) = false, want true", c)
		}
	}
	for _, c := range []string{
		"go test ./...", "git commit -m \"add .env support\"", "cat README.md",
		"ls -la internal/env/", "grep -r monkey .",
	} {
		if CommandTouchesSecret(c) {
			t.Errorf("CommandTouchesSecret(%q) = true, want false", c)
		}
	}
}

func TestCommandExposesEnvSecret(t *testing.T) {
	// Must block: commands that could print a secret env var's VALUE to stdout.
	for _, c := range []string{
		`echo "ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:+SET}${ANTHROPIC_API_KEY:-UNSET}"`, // the real leak
		"echo $GITHUB_TOKEN",
		`printf '%s\n' "$AWS_SECRET_ACCESS_KEY"`,
		`echo "${DATABASE_PASSWORD}"`,
		"printenv OPENAI_API_KEY",
		"env", // bare dump of every var
		"env | grep -i key",
		"printenv",
	} {
		if !CommandExposesEnvSecret(c) {
			t.Errorf("CommandExposesEnvSecret(%q) = false, want true", c)
		}
	}
	// Must NOT block: legit uses that never print a secret to stdout.
	for _, c := range []string{
		"go test ./...",
		`git commit -m "add API_TOKEN support"`,                   // literal mention, no expansion
		`curl -H "Authorization: Bearer $API_KEY" https://api.x/`, // uses, does not print
		"echo $HOME", "echo \"deploy done\"", // echo of a non-secret
		"env FOO=bar go test ./...", // env to RUN a command
		"printenv PATH",             // non-secret var
	} {
		if CommandExposesEnvSecret(c) {
			t.Errorf("CommandExposesEnvSecret(%q) = true, want false", c)
		}
	}
}

func TestHardenHooksGuardMatchesBash(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	found := false
	for _, e := range pre {
		m := e.(map[string]any)
		if m["matcher"] == "Write|Edit|Bash|Read" {
			found = true
		}
	}
	if !found {
		t.Error("PreToolUse secret-guard matcher must be Write|Edit|Bash|Read")
	}
}

func TestHookCommandsDegradeWithoutBinary(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks := s["hooks"].(map[string]any)
	const guard = "command -v tu-agent >/dev/null 2>&1 || exit 0;"
	for _, ev := range []string{"PreToolUse", "SessionStart", "Stop", "SessionEnd"} {
		entries := hooks[ev].([]any)
		cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if !strings.HasPrefix(cmd, guard) {
			t.Errorf("%s hook does not start with the binary-presence guard: %q", ev, cmd)
		}
	}
}

func TestSecretGuardUsesGuardPathNotJq(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	cmd := pre[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "tu-agent guard-path") {
		t.Errorf("PreToolUse should invoke tu-agent guard-path, got %q", cmd)
	}
	if strings.Contains(cmd, "jq") {
		t.Errorf("PreToolUse must not depend on jq anymore, got %q", cmd)
	}
}

func TestGitignoreBlockIgnoresSettingsBackup(t *testing.T) {
	if !strings.Contains(GitignoreBlock(), ".claude/settings.json.bak") {
		t.Error("GitignoreBlock should ignore .claude/settings.json.bak")
	}
}

// TestMergeSettings_MigratesLegacyTuAgentHooks verifies that re-running
// MergeSettings on a repo that already has tu-agent-generated hooks replaces
// them with the freshly-generated ones instead of leaving stale duplicates.
//
// Scenario: a previously hardened settings.json contains:
//   - An OLD jq-based PreToolUse secret guard (matcher "Write|Edit", command
//     contains jq + "refusing to modify secret/credential files")
//   - OLD un-guarded memory hooks: SessionStart with "tu-agent memory import
//     --quiet" (no binary guard prefix), Stop/SessionEnd with "tu-agent memory
//     export --quiet"
//   - A user's own custom PreToolUse hook (matcher "Bash", command "echo mine")
//     that must survive.
//
// After MergeSettings(old, HardenedSettings("go","go")):
//   - PreToolUse must have exactly ONE entry whose command contains
//     "tu-agent guard-path" and does NOT contain "jq".
//   - SessionStart, Stop, SessionEnd must each have exactly ONE entry, whose
//     command starts with "command -v tu-agent >/dev/null 2>&1 || exit 0;".
//   - The user's "Bash" PreToolUse hook must still be present.
func TestMergeSettings_MigratesLegacyTuAgentHooks(t *testing.T) {
	// Old jq PreToolUse entry (legacy secret guard).
	oldJqPreToolUse := map[string]any{
		"matcher": "Write|Edit",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": `jq -e '.path | test("(^|/)(\\.env($|\\.)|secrets/|id_rsa)|\\.pem$")' /dev/stdin >/dev/null 2>&1 && echo "refusing to modify secret/credential files" && exit 1; exit 0`,
		}},
	}
	// User's own PreToolUse hook — must survive.
	userBashHook := map[string]any{
		"matcher": "Bash",
		"hooks":   []any{map[string]any{"type": "command", "command": "echo mine"}},
	}
	// Old un-guarded memory hooks (no binary guard clause prefix).
	oldSessionStart := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": "command -v tu-agent >/dev/null 2>&1 || exit 0; tu-agent graph update --quiet"},
			map[string]any{"type": "command", "command": "tu-agent memory import --quiet"},
		},
	}
	oldAutoExport := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": "tu-agent memory export --quiet"}},
	}

	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse":   []any{oldJqPreToolUse, userBashHook},
			"SessionStart": []any{oldSessionStart},
			"Stop":         []any{oldAutoExport},
			"SessionEnd":   []any{oldAutoExport},
		},
	}

	merged := MergeSettings(existing, HardenedSettings("go", "go", false))

	hooks, ok := merged["hooks"].(map[string]any)
	if !ok {
		t.Fatal("merged settings has no hooks map")
	}

	// --- PreToolUse: exactly one tu-agent-managed entry (new guard-path),
	//     the user's "Bash" hook, and NO old jq entry.
	pre, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatal("PreToolUse is missing")
	}

	var guardPathEntries, jqEntries, userBashEntries []string
	for _, e := range pre {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := em["matcher"].(string)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "tu-agent guard-path") {
				guardPathEntries = append(guardPathEntries, cmd)
			}
			if strings.Contains(cmd, "jq") || strings.Contains(cmd, "refusing to modify secret/credential files") {
				jqEntries = append(jqEntries, cmd)
			}
			if matcher == "Bash" && strings.Contains(cmd, "echo mine") {
				userBashEntries = append(userBashEntries, cmd)
			}
		}
	}
	if len(guardPathEntries) != 1 {
		t.Errorf("expected exactly 1 tu-agent guard-path PreToolUse entry, got %d: %v", len(guardPathEntries), guardPathEntries)
	}
	if len(jqEntries) != 0 {
		t.Errorf("legacy jq PreToolUse entries must be removed, found %d: %v", len(jqEntries), jqEntries)
	}
	if len(userBashEntries) != 1 {
		t.Errorf("user Bash hook must be preserved exactly once, got %d", len(userBashEntries))
	}

	// --- Session hooks: exactly one entry each, with binary guard clause prefix.
	const guardPrefix = "command -v tu-agent >/dev/null 2>&1 || exit 0;"
	for _, ev := range []string{"SessionStart", "Stop", "SessionEnd"} {
		entries, ok := hooks[ev].([]any)
		if !ok || len(entries) == 0 {
			t.Fatalf("%s hook missing after merge", ev)
		}
		if len(entries) != 1 {
			t.Errorf("%s must have exactly 1 entry after migration, got %d", ev, len(entries))
		}
		entry, ok := entries[0].(map[string]any)
		if !ok {
			t.Fatalf("%s entry is not a map", ev)
		}
		inner, ok := entry["hooks"].([]any)
		if !ok || len(inner) == 0 {
			t.Fatalf("%s inner hooks missing", ev)
		}
		hm, ok := inner[0].(map[string]any)
		if !ok {
			t.Fatalf("%s inner hook is not a map", ev)
		}
		cmd, _ := hm["command"].(string)
		if !strings.HasPrefix(cmd, guardPrefix) {
			t.Errorf("%s hook command does not start with binary guard clause.\ngot:  %q\nwant prefix: %q", ev, cmd, guardPrefix)
		}
		if ev == "SessionStart" {
			var joined []string
			for _, h := range inner {
				if hm, ok := h.(map[string]any); ok {
					if c, ok := hm["command"].(string); ok {
						joined = append(joined, c)
					}
				}
			}
			all := strings.Join(joined, "\n")
			if !strings.Contains(all, "tu-agent graph update --quiet --announce") {
				t.Errorf("migrated SessionStart must carry the graph update --announce nudge; got:\n%s", all)
			}
			for _, c := range joined {
				if strings.Contains(c, "graph update --quiet") && !strings.Contains(c, "--announce") {
					t.Errorf("stale pre-announce graph update command survived migration: %q", c)
				}
			}
		}
	}
}

// TestMergeSettingsPreservesGraphFreshnessHook verifies that re-running
// MergeSettings on a settings.json that already contains a graph-freshness
// PostToolUse hook installed by "tu-agent setup --hooks" does NOT strip it.
//
// The regression: the old broad isTuAgentManagedHook predicate matched any inner
// command containing "tu-agent " (trailing space). Since hardenHooks emits a
// PostToolUse formatter entry for Go repos, unionHookEntries ran on PostToolUse
// and stripped the graph-freshness hook ("tu-agent graph update --quiet") even
// though hardenHooks never re-adds it.
func TestMergeSettingsPreservesGraphFreshnessHook(t *testing.T) {
	// Existing settings already has a graph-freshness PostToolUse entry installed
	// by "tu-agent setup --hooks".
	graphFreshnessEntry := map[string]any{
		"matcher": "Write|Edit",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": "tu-agent graph update --quiet",
		}},
	}
	existing := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{graphFreshnessEntry},
		},
	}

	// MergeSettings with a Go repo so hardenHooks emits a PostToolUse formatter
	// entry — this is the path that triggers unionHookEntries on PostToolUse.
	merged := MergeSettings(existing, HardenedSettings("go", "go", false))

	hooks, ok := merged["hooks"].(map[string]any)
	if !ok {
		t.Fatal("merged settings has no hooks map")
	}
	post, ok := hooks["PostToolUse"].([]any)
	if !ok || len(post) == 0 {
		t.Fatal("PostToolUse hooks missing after merge")
	}

	// At least one PostToolUse entry must contain the graph-freshness command.
	found := false
	for _, e := range post {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if cmd == "tu-agent graph update --quiet" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("graph-freshness hook 'tu-agent graph update --quiet' was stripped by re-harden; it must be preserved")
	}
}

func TestGitignoreBlockKeepsChunksVersioned(t *testing.T) {
	block := GitignoreBlock()
	// The DB stays ignored.
	if !strings.Contains(block, ".tu-agent/memory.db") {
		t.Fatal("memory.db must remain gitignored")
	}
	// Chunks must NOT be ignored: no line may match the chunks directory.
	for _, line := range strings.Split(block, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, ".tu-agent/memory/") || l == ".tu-agent/memory" || l == ".tu-agent/" {
			t.Fatalf("gitignore line %q would ignore committed memory chunks", l)
		}
	}
	// The block documents why chunks are versioned.
	if !strings.Contains(block, "memory/chunks") {
		t.Fatal("block should mention memory/chunks are versioned")
	}
}

func TestHardenSessionStartIncludesCrystallize(t *testing.T) {
	s := HardenedSettings("go", "go", false)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings has no hooks map")
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok || len(ss) == 0 {
		t.Fatal("no SessionStart hook")
	}
	entry := ss[0].(map[string]any)
	inner := entry["hooks"].([]any)
	var joined string
	for _, h := range inner {
		joined += h.(map[string]any)["command"].(string) + "\n"
	}
	if !strings.Contains(joined, "memory materialize") {
		t.Errorf("SessionStart must materialize crystallized skills; got:\n%s", joined)
	}
	if !strings.Contains(joined, "memory crystallize --nudge") {
		t.Errorf("SessionStart must run the crystallize nudge; got:\n%s", joined)
	}
}

// TestMergeSettingsPluginStripsSupersededHooks verifies that merging a
// pluginPresent=true generated settings into an existing settings.json that
// was hardened STANDALONE (pluginPresent=false, so it has the full
// SessionStart/Stop/SessionEnd tu-agent hooks) strips those now-superseded
// tu-agent-managed hooks. Without this, upgrading a repo via
// `prepare --update --plugin` leaves the old graph-update/memory hooks in
// settings.json alongside the plugin's own hooks.json entries, so every
// export/graph-update double-fires.
//
// The merge must visit the UNION of event keys (existing ∪ generated), not
// just the generated set — hardenHooks(pluginPresent=true) omits
// SessionStart/Stop/SessionEnd entirely, so those events must still be
// visited via the existing side to strip the stale managed entries. If
// stripping leaves an event with zero entries, the event key itself must be
// removed rather than left as an empty `[]` husk.
func TestMergeSettingsPluginStripsSupersededHooks(t *testing.T) {
	old := HardenedSettings("go", "go", false)
	hooks, ok := old["hooks"].(map[string]any)
	if !ok {
		t.Fatal("old settings has no hooks map")
	}

	// Inject a user-authored SessionStart hook (not tu-agent-managed), mimicking
	// the user-entry style used in TestMergeSettings_MigratesLegacyTuAgentHooks.
	// It must survive the merge even though the rest of SessionStart is stripped.
	userSessionStart := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": "echo user-session-start"}},
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatal("old settings has no SessionStart hook")
	}
	hooks["SessionStart"] = append(ss, userSessionStart)

	// Simulate the plugin now being present: re-hardening generates settings
	// without SessionStart/Stop/SessionEnd (the plugin's hooks.json owns those).
	generated := HardenedSettings("go", "go", true)
	merged := MergeSettings(old, generated)

	mergedHooks, ok := merged["hooks"].(map[string]any)
	if !ok {
		t.Fatal("merged settings has no hooks map")
	}

	// No tu-agent-managed entry may remain under SessionStart, Stop, or
	// SessionEnd — the plugin now owns those.
	for _, ev := range []string{"SessionStart", "Stop", "SessionEnd"} {
		entries, _ := mergedHooks[ev].([]any)
		for _, e := range entries {
			if isTuAgentManagedHook(e) {
				t.Errorf("%s still has a tu-agent-managed hook after merging pluginPresent settings: %v", ev, e)
			}
		}
	}

	// Stop and SessionEnd had ONLY tu-agent-managed entries in `old` — after
	// stripping, the event key must be removed entirely, not left as `[]`.
	for _, ev := range []string{"Stop", "SessionEnd"} {
		if v, present := mergedHooks[ev]; present {
			t.Errorf("%s should be removed entirely (no entries left), got %v", ev, v)
		}
	}

	// The user-authored SessionStart hook must survive.
	ssAfter, ok := mergedHooks["SessionStart"].([]any)
	if !ok {
		t.Fatal("SessionStart missing after merge, but the surviving user entry should have kept it alive")
	}
	foundUser := false
	for _, e := range ssAfter {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); cmd == "echo user-session-start" {
				foundUser = true
			}
		}
	}
	if !foundUser {
		t.Error("user-authored SessionStart hook was dropped by merge")
	}

	// PreToolUse (secret guard) and PostToolUse (formatter) must still be present.
	if _, ok := mergedHooks["PreToolUse"]; !ok {
		t.Error("PreToolUse secret guard missing after merge")
	}
	if _, ok := mergedHooks["PostToolUse"]; !ok {
		t.Error("PostToolUse formatter missing after merge")
	}
}

func TestHardenHooksPluginPresent(t *testing.T) {
	h := hardenHooks("go", true)
	for _, k := range []string{"SessionStart", "Stop", "SessionEnd"} {
		if _, ok := h[k]; ok {
			t.Errorf("pluginPresent hooks include %s — duplicated with plugin/hooks/hooks.json", k)
		}
	}
	if _, ok := h["PreToolUse"]; !ok {
		t.Errorf("secret guard must remain even with plugin present")
	}
	if _, ok := h["PostToolUse"]; !ok {
		t.Errorf("go formatter hook must remain even with plugin present")
	}
	// Without the plugin nothing changes.
	full := hardenHooks("go", false)
	for _, k := range []string{"SessionStart", "Stop", "SessionEnd", "PreToolUse", "PostToolUse"} {
		if _, ok := full[k]; !ok {
			t.Errorf("standalone hooks missing %s", k)
		}
	}
}
