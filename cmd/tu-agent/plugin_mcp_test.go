package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mcpServerConfig mirrors a single entry in the plugin's .mcp.json so a schema
// drift (renamed key, changed args) fails this test rather than silently
// breaking the installed plugin.
type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type pluginMCPConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

func TestPluginMCPConfig(t *testing.T) {
	path := filepath.Join("..", "..", "plugin", ".mcp.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var cfg pluginMCPConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	srv, ok := cfg.MCPServers["tu-agent-graph"]
	if !ok {
		t.Fatalf("server %q missing; have %v", "tu-agent-graph", cfg.MCPServers)
	}

	const wantCmd = "${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"
	if srv.Command != wantCmd {
		t.Errorf("command = %q, want %q", srv.Command, wantCmd)
	}

	wantArgs := []string{"mcp"}
	if !reflect.DeepEqual(srv.Args, wantArgs) {
		t.Errorf("args = %v, want %v", srv.Args, wantArgs)
	}
}
