package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// webFrameworkDeps are the package.json dependency keys that mark a repo as a
// web app. @remix-run/ is a scoped-prefix match (remix packages are
// @remix-run/*); every other entry is an exact key match.
var webFrameworkDeps = []string{
	"react",
	"next",
	"vue",
	"nuxt",
	"svelte",
	"@angular/core",
	"astro",
	"@remix-run/",
	"solid-js",
	"express",
	"fastify",
	"vite",
}

// playwrightSignalDeps are dependency keys that mark a repo as web on their
// own even without a framework present (the app already drives Playwright).
var playwrightSignalDeps = []string{
	"playwright",
	"@playwright/test",
}

var (
	playwrightPort    int
	playwrightOrigins []string
	playwrightHTTPS   bool
)

var playwrightCmd = &cobra.Command{
	Use:     "playwright",
	GroupID: "setup",
	Short:   "Detect, enable, or decline the Playwright MCP browser server",
}

// playwrightDetectOutput is the JSON shape written to cmd.OutOrStdout() by
// `tu-agent playwright detect`.
type playwrightDetectOutput struct {
	Web      bool     `json:"web"`
	Signals  []string `json:"signals"`
	Declined bool     `json:"declined"`
	Enabled  bool     `json:"enabled"`
}

var playwrightDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect whether this repo is a web app and report Playwright MCP offer state",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		web, signals, err := detectWeb(root)
		if err != nil {
			return fmt.Errorf("playwright detect: %w", err)
		}
		offer, err := readPlaywrightOffer(root)
		if err != nil {
			return fmt.Errorf("playwright detect: %w", err)
		}
		enabled, err := mcpHasPlaywrightServer(root)
		if err != nil {
			return fmt.Errorf("playwright detect: %w", err)
		}
		result := playwrightDetectOutput{
			Web:      web,
			Signals:  signals,
			Declined: offer == "declined",
			Enabled:  enabled,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("playwright detect: encoding result: %w", err)
		}
		return nil
	},
}

var playwrightEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Register the Playwright MCP server, origin-locked to the local app",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture-and-reset immediately: these are package-level vars bound to
		// local flags on a command reused across invocations (and across
		// tests), so a value set on one run would otherwise leak into the next
		// run that omits the flag.
		port := playwrightPort
		origins := playwrightOrigins
		https := playwrightHTTPS
		playwrightPort = 0
		playwrightOrigins = nil
		playwrightHTTPS = false

		if !cmd.Flags().Changed("port") || port <= 0 {
			return fmt.Errorf("playwright enable: --port is required (the local app's dev port)")
		}

		scheme := "http"
		if https {
			scheme = "https"
		}
		root := repoRoot()
		originList := make([]string, 0, len(origins)+2)
		originList = append(originList,
			fmt.Sprintf("%s://localhost:%d", scheme, port),
			fmt.Sprintf("%s://127.0.0.1:%d", scheme, port),
		)
		originList = append(originList, origins...)
		originsStr := strings.Join(originList, ";")

		if err := mergePlaywrightMCP(root, originsStr); err != nil {
			return fmt.Errorf("playwright enable: %w", err)
		}
		if err := extendSettingsForPlaywright(root); err != nil {
			return fmt.Errorf("playwright enable: %w", err)
		}
		if err := writePlaywrightOffer(root, "enabled"); err != nil {
			return fmt.Errorf("playwright enable: %w", err)
		}
		return nil
	},
}

var playwrightDeclineCmd = &cobra.Command{
	Use:   "decline",
	Short: "Record that browser verification was declined",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		if err := writePlaywrightOffer(root, "declined"); err != nil {
			return fmt.Errorf("playwright decline: %w", err)
		}
		return nil
	},
}

func init() {
	playwrightEnableCmd.Flags().IntVar(&playwrightPort, "port", 0, "the local app's dev port (required)")
	playwrightEnableCmd.Flags().StringArrayVar(&playwrightOrigins, "origin", nil, "extra allowed origin (repeatable)")
	playwrightEnableCmd.Flags().BoolVar(&playwrightHTTPS, "https", false, "seed https origins instead of http")

	playwrightCmd.AddCommand(playwrightDetectCmd, playwrightEnableCmd, playwrightDeclineCmd)
	rootCmd.AddCommand(playwrightCmd)
}

// detectWeb reports whether root looks like a web app (package.json exists and
// a known web framework or Playwright dependency is present) and the list of
// matched dependency keys. A missing or unreadable package.json is not an
// error — it simply reports web=false with no signals.
func detectWeb(root string) (bool, []string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, []string{}, nil
		}
		return false, nil, fmt.Errorf("detectWeb: reading package.json: %w", err)
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return false, nil, fmt.Errorf("detectWeb: parsing package.json: %w", err)
	}

	keys := make(map[string]struct{}, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for k := range pkg.Dependencies {
		keys[k] = struct{}{}
	}
	for k := range pkg.DevDependencies {
		keys[k] = struct{}{}
	}

	signals := make([]string, 0, len(keys))
	for _, dep := range webFrameworkDeps {
		if strings.HasSuffix(dep, "/") {
			for k := range keys {
				if strings.HasPrefix(k, dep) {
					signals = append(signals, k)
				}
			}
			continue
		}
		if _, ok := keys[dep]; ok {
			signals = append(signals, dep)
		}
	}
	for _, dep := range playwrightSignalDeps {
		if _, ok := keys[dep]; ok {
			signals = append(signals, dep)
		}
	}

	return len(signals) > 0, signals, nil
}

// mcpHasPlaywrightServer reports whether .mcp.json already registers a
// "playwright" server under root.
func mcpHasPlaywrightServer(root string) (bool, error) {
	m, err := readMCPFile(root)
	if err != nil {
		return false, err
	}
	servers, _ := m["mcpServers"].(map[string]any)
	_, ok := servers["playwright"]
	return ok, nil
}

// readMCPFile reads .mcp.json under root into a generic map, returning an
// empty map if the file does not exist.
func readMCPFile(root string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("readMCPFile: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("readMCPFile: parsing .mcp.json: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// mergePlaywrightMCP read-modify-writes .mcp.json under root, setting (create
// or overwrite) the "playwright" server entry to run the Playwright MCP
// origin-locked to origins, while preserving every other registered server.
// Unlike writeMCPConfig (which always overwrites the whole file for the
// tu-agent-graph server), this merges — re-running enable with a different
// port updates only the playwright entry's origins in place.
func mergePlaywrightMCP(root, origins string) error {
	m, err := readMCPFile(root)
	if err != nil {
		return fmt.Errorf("mergePlaywrightMCP: %w", err)
	}
	servers, _ := m["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["playwright"] = map[string]any{
		"command": "npx",
		"args":    []any{"@playwright/mcp@latest", "--allowed-origins", origins},
	}
	m["mcpServers"] = servers

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("mergePlaywrightMCP: marshaling .mcp.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("mergePlaywrightMCP: writing .mcp.json: %w", err)
	}
	return nil
}

// extendSettingsForPlaywright read-modify-writes .claude/settings.json under
// root via codegen.MergeSettings, adding "playwright" to
// enabledMcpjsonServers (the real grant mechanism, mirroring how
// tu-agent-graph is granted) and "mcp__playwright" to permissions.allow
// (Claude Code's "allow all tools of a server" pattern). MergeSettings unions
// and preserves all existing user content.
func extendSettingsForPlaywright(root string) error {
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	var existing map[string]any
	raw, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("extendSettingsForPlaywright: existing settings.json is not valid JSON: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		existing = map[string]any{}
	default:
		return fmt.Errorf("extendSettingsForPlaywright: reading settings.json: %w", err)
	}

	generated := map[string]any{
		"enabledMcpjsonServers": []any{"playwright"},
		"permissions": map[string]any{
			"allow": []any{"mcp__playwright"},
		},
	}
	merged := codegen.MergeSettings(existing, generated)
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("extendSettingsForPlaywright: marshaling settings: %w", err)
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("extendSettingsForPlaywright: creating .claude dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return fmt.Errorf("extendSettingsForPlaywright: writing settings.json: %w", err)
	}
	return nil
}

// playwrightOfferSchemaVersion is the current .tu-agent/playwright.json
// schema version. Readers tolerate its absence (pre-version files).
const playwrightOfferSchemaVersion = 1

// playwrightOfferState is the shape of .tu-agent/playwright.json.
type playwrightOfferState struct {
	Offer   string `json:"offer"`
	At      string `json:"at"`
	Version int    `json:"version"`
}

// readPlaywrightOffer returns the recorded offer ("declined"/"enabled") for
// root, or "" if no offer has been recorded yet.
func readPlaywrightOffer(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(tuAgentDir(root), "playwright.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("readPlaywrightOffer: %w", err)
	}
	var state playwrightOfferState
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", fmt.Errorf("readPlaywrightOffer: parsing playwright.json: %w", err)
	}
	return state.Offer, nil
}

// writePlaywrightOffer records the offer state for root.
func writePlaywrightOffer(root, offer string) error {
	dir := tuAgentDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writePlaywrightOffer: creating .tu-agent dir: %w", err)
	}
	state := playwrightOfferState{Offer: offer, At: time.Now().UTC().Format(time.RFC3339), Version: playwrightOfferSchemaVersion}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("writePlaywrightOffer: marshaling playwright.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playwright.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writePlaywrightOffer: writing playwright.json: %w", err)
	}
	return nil
}
