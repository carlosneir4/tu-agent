package main

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAdviseToolRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["advise"] {
		t.Error("newMCPServer does not serve \"advise\"")
	}
}

// TestMCP_adviseRoundTrip guards the read-only diagnostic shape: it must
// surface a threshold-meeting suggestion without persisting any dedup state
// (unlike advise --nudge), so calling it twice in a row prints the same
// suggestion both times.
func TestMCP_adviseRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })
	seedCrystallizeCluster(t, repoRoot())

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	out := callTool(t, session, "advise", map[string]any{})
	if !strings.Contains(out, "tu-agent memory crystallize") {
		t.Errorf("advise tool output missing crystallize suggestion: %q", out)
	}

	// No dedup state persisted: calling it again returns the same suggestion.
	out2 := callTool(t, session, "advise", map[string]any{})
	if out2 != out {
		t.Errorf("advise MCP tool must not persist dedup state; first=%q second=%q", out, out2)
	}
}

func TestMCP_adviseEmptyWhenNoEvidence(t *testing.T) {
	t.Chdir(t.TempDir())

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	out := callTool(t, session, "advise", map[string]any{})
	for _, unwanted := range []string{"crystallize", "secret-guard", "get_context", "mem_save"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("advise with no evidence: unexpected suggestion content %q in %q", unwanted, out)
		}
	}
}
