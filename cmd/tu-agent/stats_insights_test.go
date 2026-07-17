package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatsInsights_ReportsUnusedToolsAndZeroResultRate(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".tu-agent", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"timestamp":"2026-01-01T00:00:00Z","event":"mcp_call","tool":"mem_search","duration_ms":100,"result_bytes":50,"zero_result":true}
{"timestamp":"2026-01-01T00:00:01Z","event":"mcp_call","tool":"mem_search","duration_ms":200,"result_bytes":150}
{"timestamp":"2026-01-01T00:00:02Z","event":"graph_refresh","parsed":5,"unchanged":2,"deleted":1,"failed":0,"ok":true}
{"timestamp":"2026-01-01T00:00:03Z","event":"hook","tool":"graph update","duration_ms":30,"ok":true}
`
	if err := os.WriteFile(filepath.Join(".tu-agent", "logs", "telemetry.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	statsInsights = true
	t.Cleanup(func() { statsInsights = false })

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats --insights: %v", err)
	}

	if !strings.Contains(out, "mem_search") {
		t.Errorf("report must mention mem_search, got:\n%s", out)
	}
	// mem_search: 1 of 2 calls zero-result -> 50%.
	if !strings.Contains(out, "50") {
		t.Errorf("report must mention the mem_search zero-result rate (50%%), got:\n%s", out)
	}
	// get_context (a registered MCP tool) never appears in the entries -> unused.
	if !strings.Contains(out, "get_context") {
		t.Errorf("report must list get_context as an unused tool, got:\n%s", out)
	}
}
