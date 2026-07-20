package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// mcpActionHookCmd locates the `hook mcp-action` subcommand on hookCmd at
// runtime. It does not exist on the pre-change tree, so this lookup — not a
// direct symbol reference — is the RED signal for @s1/@s2: the file compiles
// against the old tree and fails at runtime via t.Fatal.
func mcpActionHookCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range hookCmd.Commands() {
		if c.Use == "mcp-action" {
			return c
		}
	}
	t.Fatal("hook mcp-action subcommand not registered on hookCmd")
	return nil
}

// runMCPActionHook drives the mcp-action hook's RunE with stdin swapped to
// payload, returning the error RunE reports.
func runMCPActionHook(t *testing.T, payload string) error {
	t.Helper()
	c := mcpActionHookCmd(t)
	c.SetIn(strings.NewReader(payload))
	return c.RunE(c, nil)
}

// @s1: the mcp-action hook records a browser tool call at telemetry level
// "minimal" — this is the audit-trail security property (F3): it must not be
// gated behind the "full" tier the way prompt-submit/prompt-expansion are.
func TestMCPActionHook_MinimalLevelRecordsRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	payload := `{"session_id":"sess-1","tool_name":"mcp__playwright__browser_navigate"}`
	if err := runMCPActionHook(t, payload); err != nil {
		t.Fatalf("@s1: RunE err = %v, want nil", err)
	}

	data := readTelemetryFile(t, root)
	if got := countLinesContaining(data, `"event":"mcp_call"`); got != 1 {
		t.Fatalf("@s1: expected exactly one mcp_call line at minimal level, got %d in: %s", got, data)
	}
	line := string(data)
	if !strings.Contains(line, `"tool":"mcp__playwright__browser_navigate"`) {
		t.Errorf("@s1: line missing tool field naming the browser tool, got: %s", line)
	}
	if !strings.Contains(line, `"session_id":"sess-1"`) {
		t.Errorf("@s1: line missing session_id field, got: %s", line)
	}
}

// @s2: a malformed hook payload exits clean with no row and no error.
func TestMCPActionHook_MalformedPayloadNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if err := runMCPActionHook(t, "not json"); err != nil {
		t.Fatalf("@s2: RunE err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("@s2: malformed payload must not write a row, got: %s", data)
	}
}

// @s2b (same scenario, empty tool_name branch): a payload with no tool_name
// is also a silent no-op — mirrors the task contract's "silent no-op
// otherwise" clause for a well-formed but empty tool_name.
func TestMCPActionHook_EmptyToolNameNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if err := runMCPActionHook(t, `{"session_id":"sess-1","tool_name":""}`); err != nil {
		t.Fatalf("@s2: RunE err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("@s2: empty tool_name must not write a row, got: %s", data)
	}
}

// @s3: hooks.json wires the mcp-action hook for playwright tools, and the
// existing Write|Edit / Bash entries are unchanged. loadPluginHooks and
// pluginHooksConfig are the existing loader (plugin_hooks_test.go, same
// package) — reused here for parity checking, not redefined.
func TestPluginHooksConfig_MCPActionMatcher(t *testing.T) {
	cfg := loadPluginHooks(t)

	var mcpEntry *pluginHookEntry
	for i, e := range cfg.Hooks["PostToolUse"] {
		if e.Matcher == "mcp__playwright" {
			mcpEntry = &cfg.Hooks["PostToolUse"][i]
		}
	}
	if mcpEntry == nil {
		t.Fatal(`@s3: PostToolUse has no "mcp__playwright" matcher entry`)
	}
	found := false
	for _, h := range mcpEntry.Hooks {
		if strings.Contains(h.Command, "hook mcp-action") {
			found = true
			if !strings.HasSuffix(h.Command, " || exit 0") {
				t.Errorf("@s3: mcp-action command %q must end in \" || exit 0\"", h.Command)
			}
		}
	}
	if !found {
		t.Errorf("@s3: mcp__playwright entry hooks = %v, want one running \"hook mcp-action\"", mcpEntry.Hooks)
	}

	// The pre-existing Write|Edit and Bash entries must be untouched.
	writeEditCmds := ""
	bashCmds := ""
	for _, e := range cfg.Hooks["PostToolUse"] {
		switch e.Matcher {
		case "Write|Edit":
			for _, h := range e.Hooks {
				writeEditCmds += h.Command + "\n"
			}
		case "Bash":
			for _, h := range e.Hooks {
				bashCmds += h.Command + "\n"
			}
		}
	}
	if !strings.Contains(writeEditCmds, "graph update --quiet") || !strings.Contains(writeEditCmds, "hook edit-check") {
		t.Errorf("@s3: Write|Edit entry changed unexpectedly, got: %q", writeEditCmds)
	}
	if !strings.Contains(bashCmds, "graph update --post-bash") {
		t.Errorf("@s3: Bash entry changed unexpectedly, got: %q", bashCmds)
	}
}

// @s4: detect reports enabled:true after enable ran. NOTE (regression pin):
// mcpHasPlaywrightServer already reads .mcp.json for a "playwright" key and
// enable already writes that key today — this scenario may already be GREEN
// on the pre-change tree. It is kept here (not dropped) so a future
// regression on the enabled-detection path still fails a named scenario.
func TestPlaywrightDetect_EnabledTrueAfterEnable(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writePackageJSON(t, dir, `{"dependencies":{"next":"^14.0.0"}}`)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s4: tu-agent playwright enable --port 3000: %v", err)
	}

	out, err := runPlaywrightCLI(t, "playwright", "detect")
	if err != nil {
		t.Fatalf("@s4: tu-agent playwright detect: %v", err)
	}
	var got playwrightDetectResult
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("@s4: detect output not valid JSON (%q): %v", out, uerr)
	}
	if !got.Enabled {
		t.Errorf("@s4: detect enabled = %v, want true after enable ran", got.Enabled)
	}
}

// @s5: enable --https seeds https origins instead of http. The --https flag
// does not exist on playwrightEnableCmd today, so this fails at runtime with
// an "unknown flag" error from cobra — the RED signal.
func TestPlaywrightEnable_HTTPSSeedsHTTPSOrigins(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000", "--https"); err != nil {
		t.Fatalf("@s5: tu-agent playwright enable --port 3000 --https: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	pw, ok := servers["playwright"].(map[string]any)
	if !ok {
		t.Fatalf("@s5: .mcp.json missing \"playwright\" server: %v", mcp)
	}
	args := toStringSlice(pw["args"])
	want := "https://localhost:3000;https://127.0.0.1:3000"
	if !containsSeq(args, "--allowed-origins", want) {
		t.Errorf("@s5: args = %v, want \"--allowed-origins\" followed by exactly %q", args, want)
	}
	stale := "http://localhost:3000;http://127.0.0.1:3000"
	if containsSeq(args, "--allowed-origins", stale) {
		t.Errorf("@s5: args = %v, --https must not seed http origins", args)
	}
}

// @s6: playwright.json carries a schema version. playwrightOfferState today
// is {offer, at} with no version field, so the parsed map never has a
// "version" key — the RED signal.
func TestPlaywrightOfferState_HasSchemaVersion(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, dir string)
	}{
		{
			name: "after enable",
			run: func(t *testing.T, dir string) {
				if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
					t.Fatalf("playwright enable --port 3000: %v", err)
				}
			},
		},
		{
			name: "after decline",
			run: func(t *testing.T, dir string) {
				if _, err := runPlaywrightCLI(t, "playwright", "decline"); err != nil {
					t.Fatalf("playwright decline: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			chdir(t, dir)
			tc.run(t, dir)

			state := readPlaywrightOfferState(t, dir)
			version, ok := state["version"]
			if !ok {
				t.Fatalf("@s6: .tu-agent/playwright.json has no \"version\" field: %v", state)
			}
			if version != float64(1) {
				t.Errorf("@s6: .tu-agent/playwright.json version = %v, want 1", version)
			}
		})
	}
}

// prepareSkillPath is the repo-relative path to the prepare skill prose,
// resolved from cmd/tu-agent the same way loadPluginHooks resolves
// plugin/hooks/hooks.json.
const prepareSkillPath = "../../plugin/skills/prepare/SKILL.md"

// readPrepareSkill reads plugin/skills/prepare/SKILL.md.
func readPrepareSkill(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(prepareSkillPath)
	if err != nil {
		t.Fatalf("reading %s: %v", prepareSkillPath, err)
	}
	return string(raw)
}

// skillSection extracts the lines from the first line with the given heading
// prefix up to (but excluding) the next top-level ("## ") heading, or EOF.
func skillSection(content, headingPrefix string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, headingPrefix) {
			start = i
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// @s7: the prepare report (Step 5) mentions the browser-verification
// decision. Today Step 5 summarizes only files/learn/agents — no mention of
// "browser" — the RED signal.
func TestPrepareSkill_Step5MentionsBrowserDecision(t *testing.T) {
	section := skillSection(readPrepareSkill(t), "## Step 5:")
	if section == "" {
		t.Fatal("@s7: SKILL.md has no \"## Step 5:\" section")
	}
	if !strings.Contains(strings.ToLower(section), "browser") {
		t.Errorf("@s7: Step 5 section does not mention the browser-verification decision:\n%s", section)
	}
}

// @s8: the offer step (Step 1.5) tells a user who does not know their dev
// port how to find it (package.json scripts / dev-server config) or defer.
// Today Step 1.5 only says "Ask for the app's dev port" with no fallback —
// the RED signal.
func TestPrepareSkill_OfferHelpsFindPort(t *testing.T) {
	section := skillSection(readPrepareSkill(t), "## Step 1.5:")
	if section == "" {
		t.Fatal("@s8: SKILL.md has no \"## Step 1.5:\" section")
	}
	lower := strings.ToLower(section)
	if !strings.Contains(lower, "package.json") {
		t.Errorf("@s8: Step 1.5 section does not mention finding the port via package.json scripts:\n%s", section)
	}
	if !strings.Contains(lower, "defer") {
		t.Errorf("@s8: Step 1.5 section does not mention deferring when the port is unknown:\n%s", section)
	}
}

// @s9: the offer step is transparent that .mcp.json and settings.json are
// repo-committed and travel to teammates. Today the offer bullets list what
// gets written but never say these files are conventionally committed — the
// RED signal.
func TestPrepareSkill_OfferWarnsFilesAreCommitted(t *testing.T) {
	section := skillSection(readPrepareSkill(t), "## Step 1.5:")
	if section == "" {
		t.Fatal("@s9: SKILL.md has no \"## Step 1.5:\" section")
	}
	lower := strings.ToLower(section)
	if !strings.Contains(lower, "commit") {
		t.Errorf("@s9: Step 1.5 section does not warn that .mcp.json/settings.json are repo-committed:\n%s", section)
	}
	if !strings.Contains(lower, "teammate") && !strings.Contains(lower, "team member") {
		t.Errorf("@s9: Step 1.5 section does not say these files travel to teammates:\n%s", section)
	}
}

// @s10: the offer acknowledges that detect may over-match backend servers,
// and (already true today) that declining is recorded. Today the section has
// no over-match caveat at all — the RED signal.
func TestPrepareSkill_OfferAcknowledgesBackendOverMatch(t *testing.T) {
	section := skillSection(readPrepareSkill(t), "## Step 1.5:")
	if section == "" {
		t.Fatal("@s10: SKILL.md has no \"## Step 1.5:\" section")
	}
	lower := strings.ToLower(section)
	if !strings.Contains(lower, "backend") && !strings.Contains(lower, "over-match") && !strings.Contains(lower, "false positive") {
		t.Errorf("@s10: Step 1.5 section does not note that detect may over-match backend servers:\n%s", section)
	}
	if !strings.Contains(lower, "recorded") {
		t.Errorf("@s10: Step 1.5 section does not state that declining is recorded:\n%s", section)
	}
}
