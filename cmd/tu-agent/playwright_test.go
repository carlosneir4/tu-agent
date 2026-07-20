package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// playwrightDetectResult mirrors the JSON `tu-agent playwright detect` must
// print to cmd.OutOrStdout(): {"web":bool,"signals":[...],"declined":bool,"enabled":bool}.
type playwrightDetectResult struct {
	Web      bool     `json:"web"`
	Signals  []string `json:"signals"`
	Declined bool     `json:"declined"`
	Enabled  bool     `json:"enabled"`
}

// runPlaywrightCLI executes rootCmd with args against a fresh output buffer
// and returns what was written to cmd.OutOrStdout() plus the Execute error.
// Today "playwright" is not a registered subcommand, so every call returns a
// non-nil "unknown command" error and an empty buffer — the honest RED state.
//
// rootCmd is a package-level singleton shared with every other test file, and
// cobra's OutOrStdout()/ErrOrStderr() fall back through the parent chain when
// a subcommand has no writer of its own — so a writer left on rootCmd leaks
// into unrelated tests elsewhere in the suite. t.Cleanup restores the nil
// (default os.Stdout/os.Stderr) writers once this test is done with them.
func runPlaywrightCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

func writePackageJSON(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing package.json: %v", err)
	}
}

func writeExistingMCPConfig(t *testing.T, dir string) {
	t.Helper()
	content := `{
  "mcpServers": {
    "tu-agent-graph": {
      "command": "/usr/local/bin/tu-agent",
      "args": ["mcp"]
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing .mcp.json: %v", err)
	}
}

func writeExistingSettings(t *testing.T, dir string) {
	t.Helper()
	settingsDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{
  "permissions": {
    "allow": ["Bash(ls*)"]
  }
}
`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing .claude/settings.json: %v", err)
	}
}

func readMCPJSON(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json not written: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf(".mcp.json invalid JSON: %v", err)
	}
	return m
}

func readSettingsJSON(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf(".claude/settings.json not written: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf(".claude/settings.json invalid JSON: %v", err)
	}
	return m
}

func readPlaywrightOfferState(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".tu-agent", "playwright.json"))
	if err != nil {
		t.Fatalf(".tu-agent/playwright.json not written: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf(".tu-agent/playwright.json invalid JSON: %v", err)
	}
	return m
}

func toStringSlice(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// containsSeq reports whether a is immediately followed by b somewhere in args.
func containsSeq(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

func stringSliceContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func countOccurrences(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}

func TestPlaywrightDetect(t *testing.T) {
	cases := []struct {
		name        string
		tag         string
		packageJSON string
		wantWeb     bool
		wantSignal  string
	}{
		{
			name:        "react dependency reports web",
			tag:         "@s1",
			packageJSON: `{"dependencies":{"react":"^18.0.0"}}`,
			wantWeb:     true,
			wantSignal:  "react",
		},
		{
			name:        "non-web dependency reports not web",
			tag:         "@s2",
			packageJSON: `{"dependencies":{"lodash":"^4.17.21"}}`,
			wantWeb:     false,
		},
		{
			name:        "playwright-only devDependency reports web",
			tag:         "@s3",
			packageJSON: `{"devDependencies":{"@playwright/test":"^1.40.0"}}`,
			wantWeb:     true,
			wantSignal:  "@playwright/test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			chdir(t, dir)
			writePackageJSON(t, dir, tc.packageJSON)

			out, err := runPlaywrightCLI(t, "playwright", "detect")
			if err != nil {
				t.Fatalf("%s: tu-agent playwright detect: %v", tc.tag, err)
			}
			var got playwrightDetectResult
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("%s: detect output not valid JSON (%q): %v", tc.tag, out, err)
			}
			if got.Web != tc.wantWeb {
				t.Errorf("%s: web = %v, want %v", tc.tag, got.Web, tc.wantWeb)
			}
			if tc.wantSignal != "" && !stringSliceContains(got.Signals, tc.wantSignal) {
				t.Errorf("%s: signals = %v, want to contain %q", tc.tag, got.Signals, tc.wantSignal)
			}
		})
	}
}

func TestPlaywrightEnable_WritesOriginLockedEntry(t *testing.T) { // @s4
	dir := t.TempDir()
	chdir(t, dir)
	writePackageJSON(t, dir, `{"dependencies":{"next":"^14.0.0"}}`)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s4: tu-agent playwright enable --port 3000: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	pw, ok := servers["playwright"].(map[string]any)
	if !ok {
		t.Fatalf("@s4: .mcp.json missing \"playwright\" server: %v", mcp)
	}
	if cmdVal, _ := pw["command"].(string); cmdVal != "npx" {
		t.Errorf("@s4: playwright command = %q, want \"npx\"", cmdVal)
	}
	args := toStringSlice(pw["args"])
	want := "http://localhost:3000;http://127.0.0.1:3000"
	if !containsSeq(args, "--allowed-origins", want) {
		t.Errorf("@s4: args = %v, want \"--allowed-origins\" followed by %q", args, want)
	}
}

func TestPlaywrightEnable_PreservesExistingServer(t *testing.T) { // @s5
	dir := t.TempDir()
	chdir(t, dir)
	writeExistingMCPConfig(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s5: tu-agent playwright enable --port 3000: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	if _, ok := servers["tu-agent-graph"]; !ok {
		t.Errorf("@s5: .mcp.json lost the \"tu-agent-graph\" server: %v", servers)
	}
	if _, ok := servers["playwright"]; !ok {
		t.Errorf("@s5: .mcp.json missing the \"playwright\" server: %v", servers)
	}
}

func TestPlaywrightEnable_ExtendsSettingsAllowlist(t *testing.T) { // @s6
	dir := t.TempDir()
	chdir(t, dir)
	writeExistingSettings(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s6: tu-agent playwright enable --port 3000: %v", err)
	}

	settings := readSettingsJSON(t, dir)
	servers := toStringSlice(settings["enabledMcpjsonServers"])
	if !stringSliceContains(servers, "playwright") {
		t.Errorf("@s6: enabledMcpjsonServers = %v, want to contain \"playwright\"", servers)
	}
	perms, _ := settings["permissions"].(map[string]any)
	allow := toStringSlice(perms["allow"])
	if !stringSliceContains(allow, "mcp__playwright") {
		t.Errorf("@s6: permissions.allow = %v, want to contain \"mcp__playwright\"", allow)
	}
	if !stringSliceContains(allow, "Bash(ls*)") {
		t.Errorf("@s6: permissions.allow = %v, want to still contain \"Bash(ls*)\"", allow)
	}
}

func TestPlaywrightEnable_RerunUpdatesOriginsInPlace(t *testing.T) { // @s7
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s7: first enable --port 3000: %v", err)
	}
	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "4000"); err != nil {
		t.Fatalf("@s7: second enable --port 4000: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	pw, ok := servers["playwright"].(map[string]any)
	if !ok {
		t.Fatalf("@s7: .mcp.json missing \"playwright\" server: %v", mcp)
	}
	args := toStringSlice(pw["args"])
	want := "http://localhost:4000;http://127.0.0.1:4000"
	if !containsSeq(args, "--allowed-origins", want) {
		t.Errorf("@s7: args = %v, want allowed-origins exactly %q", args, want)
	}
	stale := "http://localhost:3000;http://127.0.0.1:3000"
	if containsSeq(args, "--allowed-origins", stale) {
		t.Errorf("@s7: args = %v, stale port-3000 origins were not replaced", args)
	}

	settings := readSettingsJSON(t, dir)
	serverList := toStringSlice(settings["enabledMcpjsonServers"])
	if got := countOccurrences(serverList, "playwright"); got != 1 {
		t.Errorf("@s7: enabledMcpjsonServers contains \"playwright\" %d time(s), want exactly once (%v)", got, serverList)
	}
}

func TestPlaywrightEnable_AppendsExtraOrigins(t *testing.T) { // @s8
	dir := t.TempDir()
	chdir(t, dir)
	writePackageJSON(t, dir, `{"dependencies":{"vite":"^5.0.0"}}`)

	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000", "--origin", "http://localhost:5173"); err != nil {
		t.Fatalf("@s8: tu-agent playwright enable --port 3000 --origin http://localhost:5173: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	pw, ok := servers["playwright"].(map[string]any)
	if !ok {
		t.Fatalf("@s8: .mcp.json missing \"playwright\" server: %v", mcp)
	}
	args := toStringSlice(pw["args"])
	want := "http://localhost:3000;http://127.0.0.1:3000;http://localhost:5173"
	if !containsSeq(args, "--allowed-origins", want) {
		t.Errorf("@s8: args = %v, want allowed-origins %q", args, want)
	}
}

func TestPlaywrightDecline_RecordsOfferAndDetectReflectsIt(t *testing.T) { // @s9
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "decline"); err != nil {
		t.Fatalf("@s9: tu-agent playwright decline: %v", err)
	}

	offer := readPlaywrightOfferState(t, dir)
	if got, _ := offer["offer"].(string); got != "declined" {
		t.Errorf("@s9: .tu-agent/playwright.json offer = %q, want \"declined\"", got)
	}

	out, err := runPlaywrightCLI(t, "playwright", "detect")
	if err != nil {
		t.Fatalf("@s9: tu-agent playwright detect: %v", err)
	}
	var got playwrightDetectResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("@s9: detect output not valid JSON (%q): %v", out, err)
	}
	if !got.Declined {
		t.Errorf("@s9: detect declined = %v, want true", got.Declined)
	}
}

func TestPlaywrightEnable_WorksAfterDecline(t *testing.T) { // @s10
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := runPlaywrightCLI(t, "playwright", "decline"); err != nil {
		t.Fatalf("@s10: tu-agent playwright decline: %v", err)
	}
	if _, err := runPlaywrightCLI(t, "playwright", "enable", "--port", "3000"); err != nil {
		t.Fatalf("@s10: tu-agent playwright enable --port 3000: %v", err)
	}

	mcp := readMCPJSON(t, dir)
	servers, _ := mcp["mcpServers"].(map[string]any)
	if _, ok := servers["playwright"]; !ok {
		t.Errorf("@s10: .mcp.json missing \"playwright\" server: %v", servers)
	}

	offer := readPlaywrightOfferState(t, dir)
	if got, _ := offer["offer"].(string); got != "enabled" {
		t.Errorf("@s10: .tu-agent/playwright.json offer = %q, want \"enabled\"", got)
	}
}

func TestPlaywrightEnable_MissingPortErrors(t *testing.T) { // @s11
	dir := t.TempDir()
	chdir(t, dir)

	_, err := runPlaywrightCLI(t, "playwright", "enable")
	if err == nil {
		t.Fatal("@s11: tu-agent playwright enable with no --port: expected a non-zero error, got nil")
	}
	if !strings.Contains(err.Error(), "--port") {
		t.Errorf("@s11: error = %q, want it to mention \"--port\"", err.Error())
	}
}
