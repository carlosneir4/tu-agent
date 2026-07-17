package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func buildFixtureGraph(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("buildFixtureGraph: mkdir: %v", err)
	}
	src := "package shop\ntype Widget struct{}\nfunc (w *Widget) Price() int { return 1 }\n"
	if err := os.WriteFile("widget.go", []byte(src), 0o644); err != nil {
		t.Fatalf("buildFixtureGraph: write widget.go: %v", err)
	}
	st, err := store.Open(filepath.Join(".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("buildFixtureGraph: store.Open: %v", err)
	}
	if _, err := extract.Build(".", extract.Extensions(), st); err != nil {
		t.Fatalf("buildFixtureGraph: extract.Build: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("buildFixtureGraph: store.Close: %v", err)
	}
}

func TestMCP_findSymbolRoundtrip(t *testing.T) {
	t.Chdir(t.TempDir())
	buildFixtureGraph(t)

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

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_symbol",
		Arguments: map[string]any{"symbol": "Widget"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "Widget") || !strings.Contains(text, "widget.go") {
		t.Fatalf("find_symbol result missing Widget or widget.go; got:\n%s", text)
	}
}

func TestMCP_badTargetIsToolError(t *testing.T) {
	t.Chdir(t.TempDir())
	buildFixtureGraph(t)

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

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_symbol",
		Arguments: map[string]any{"symbol": "NoSuchSymbolXYZ"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected a content body even for an empty match")
	}
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}

func TestMCP_memSaveUpsertRoundtrip(t *testing.T) {
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

	out := callTool(t, session, "mem_save",
		map[string]any{"topic": "architecture/auth", "content": "v1"})
	if !strings.Contains(out, "rev:1") {
		t.Errorf("first mem_save = %q, want rev:1", out)
	}
	out = callTool(t, session, "mem_save",
		map[string]any{"topic": "architecture/auth", "content": "v2"})
	if !strings.Contains(out, "rev:2") {
		t.Errorf("second mem_save = %q, want rev:2", out)
	}

	out = callTool(t, session, "mem_search", map[string]any{"query": "v2"})
	if !strings.Contains(out, "architecture/auth") {
		t.Errorf("mem_search = %q, want topic match", out)
	}

	out = callTool(t, session, "mem_recent", map[string]any{"n": 5})
	if !strings.Contains(out, "v2") {
		t.Errorf("mem_recent = %q, want latest content", out)
	}
}

// TestMCP_memSearchLimitMarker guards the default LIMIT + disclosure for the
// MCP tool: a broad query on a mature memory DB must not dump every match
// into the caller's context, and the truncation must be disclosed.
func TestMCP_memSearchLimitMarker(t *testing.T) {
	t.Chdir(t.TempDir())

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		topic := fmt.Sprintf("decision/topic-%02d", i)
		if _, err := ms.Upsert(topic, "caplimit body", memory.UpsertOpts{Type: "decision"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

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

	out := callTool(t, session, "mem_search", map[string]any{"query": "caplimit"})
	if !strings.Contains(out, "showing 20 of 25") {
		t.Errorf("mem_search = %q, want truncation marker 'showing 20 of 25'", out)
	}
	if !strings.Contains(out, "raise limit") {
		t.Errorf("mem_search = %q, want 'raise limit' wording", out)
	}
}

// servedToolNames connects an in-memory MCP client to newMCPServer() and
// returns the set of tool names the server ACTUALLY serves, enumerated via
// ClientSession.Tools. This is the ground truth (set R) that both the
// registry-derived name list and the `--list` output must match — it is
// derived from the real AddTool registrations, so it cannot be compared
// against itself the way the old, vacuous manual-slice-vs-manual-slice test
// was.
func servedToolNames(t *testing.T) map[string]bool {
	t.Helper()
	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	served := map[string]bool{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("enumerate tools: %v", err)
		}
		served[tool.Name] = true
	}
	if len(served) == 0 {
		t.Fatal("newMCPServer served no tools")
	}
	return served
}

// TestMCPListMatchesRegisteredTools is the genuine drift guard (@s1 + @s2).
// It compares the tools ACTUALLY served by newMCPServer (set R, enumerated via
// the MCP client) against the registry-derived names returned by
// buildMCPServer (set P). The two sets must be equal — no served tool missing
// from --list, and no --list name that is not actually served.
//
//   - @s1 (RED): under the conductor's stub, buildMCPServer returns a nil/empty
//     name list, so P is empty while R holds every registered tool; the sets
//     differ and this test fails.
//   - @s2 (GREEN): once buildMCPServer accumulates each name via addTool and
//     returns the real registry, P == R exactly and the test passes.
func TestMCPListMatchesRegisteredTools(t *testing.T) {
	t.Chdir(t.TempDir())

	served := servedToolNames(t) // set R

	_, names := buildMCPServer() // set P — registry-derived
	derived := map[string]bool{}
	for _, n := range names {
		derived[n] = true
	}

	var missing, extra []string
	for n := range served {
		if !derived[n] {
			missing = append(missing, n)
		}
	}
	for n := range derived {
		if !served[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("registry-derived --list names drift from the tools actually served:\n"+
			"  missing from --list (served but not derived): %v\n"+
			"  extra in --list (derived but not served):     %v",
			missing, extra)
	}
}

// TestMCPListOutputMatchesServedTools covers @s3: with the manual name slice
// gone, printMCPTools must derive its output from the registry, so `--list`
// prints exactly the served tool names — every served name present, no extra
// lines, and an equal count. The expectation is anchored on the
// client-enumerated set R (never on any hand-maintained slice), so `--list`
// can no longer drift from what is served.
func TestMCPListOutputMatchesServedTools(t *testing.T) {
	t.Chdir(t.TempDir())

	served := servedToolNames(t) // set R

	var buf strings.Builder
	printMCPTools(&buf)

	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			listed[line] = true
		}
	}

	for name := range served {
		if !listed[name] {
			t.Errorf("mcp --list output missing served tool %q:\n%s", name, buf.String())
		}
	}
	for name := range listed {
		if !served[name] {
			t.Errorf("mcp --list output lists %q, which newMCPServer does not serve", name)
		}
	}
	if len(listed) != len(served) {
		t.Errorf("mcp --list prints %d tools, want %d (the served set)", len(listed), len(served))
	}
}

// writeJavaTraitsFixture writes a minimal fictional Java service with an
// interface and two implementers, then builds the graph from it.
func writeJavaTraitsFixture(t *testing.T) {
	t.Helper()
	files := map[string]string{
		"Refundable.java":   "package shop;\npublic interface Refundable {\n    void refund();\n}\n",
		"Order.java":        "package shop;\npublic class Order implements Refundable {\n    public void refund() {}\n}\n",
		"Subscription.java": "package shop;\npublic class Subscription implements Refundable {\n    public void refund() {}\n}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(name, []byte(src), 0o644); err != nil {
			t.Fatalf("writeJavaTraitsFixture: %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("writeJavaTraitsFixture: mkdir: %v", err)
	}
	st, err := store.Open(filepath.Join(".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("writeJavaTraitsFixture: store.Open: %v", err)
	}
	if _, err := extract.Build(".", extract.Extensions(), st); err != nil {
		t.Fatalf("writeJavaTraitsFixture: extract.Build: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("writeJavaTraitsFixture: store.Close: %v", err)
	}
}

func TestRunGraphTraits_ResolvesAndAssembles(t *testing.T) {
	t.Chdir(t.TempDir())
	writeJavaTraitsFixture(t)

	res, err := runGraphTraits("Refundable", 2, 50)
	if err != nil {
		t.Fatalf("runGraphTraits: %v", err)
	}
	if res.AsInterface == nil || len(res.AsInterface.Implementers) != 2 {
		t.Fatalf("AsInterface = %+v, want Order and Subscription as implementers", res.AsInterface)
	}

	res, err = runGraphTraits("Order", 2, 50)
	if err != nil {
		t.Fatalf("runGraphTraits(Order): %v", err)
	}
	if len(res.AsType) != 1 || res.AsType[0].Interface.Name != "Refundable" {
		t.Fatalf("AsType = %+v, want [Refundable]", res.AsType)
	}
}

func TestMCP_getTraitsParityWithCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	writeJavaTraitsFixture(t)

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

	// By-interface: Refundable should list implementers (JSON field names are lower-case).
	text := callTool(t, session, "get_traits", map[string]any{"symbol": "Refundable", "depth": 2, "max_results": 50})
	for _, want := range []string{"Refundable", "implementers", "Order", "Subscription"} {
		if !strings.Contains(text, want) {
			t.Errorf("get_traits(Refundable) missing %q in:\n%s", want, text)
		}
	}

	// By-type: Order should list Refundable as the implemented interface.
	text = callTool(t, session, "get_traits", map[string]any{"symbol": "Order", "depth": 2, "max_results": 50})
	if !strings.Contains(text, "Refundable") {
		t.Errorf("get_traits(Order) missing Refundable:\n%s", text)
	}
}

// writeGoFlowFixture writes a fictional Go ingest pipeline where Start calls
// transform which calls emit, producing a 3-hop call chain.
func writeGoFlowFixture(t *testing.T) {
	t.Helper()
	src := `package ingest

type IngestPipeline struct{}

func (p *IngestPipeline) Start(data string) {
	p.transform(data)
}

func (p *IngestPipeline) transform(data string) {
	p.emit(data)
}

func (p *IngestPipeline) emit(data string) {}
`
	if err := os.WriteFile("ingest.go", []byte(src), 0o644); err != nil {
		t.Fatalf("writeGoFlowFixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("writeGoFlowFixture: mkdir: %v", err)
	}
	st, err := store.Open(filepath.Join(".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("writeGoFlowFixture: store.Open: %v", err)
	}
	if _, err := extract.Build(".", extract.Extensions(), st); err != nil {
		t.Fatalf("writeGoFlowFixture: extract.Build: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("writeGoFlowFixture: store.Close: %v", err)
	}
}

func TestRunGraphFlow_TraversesCalls(t *testing.T) {
	t.Chdir(t.TempDir())
	writeGoFlowFixture(t)

	res, err := runGraphFlow("Start", 5, 10)
	if err != nil {
		t.Fatalf("runGraphFlow: %v", err)
	}
	if res.Entry.Name == "" {
		t.Fatal("entry name is empty")
	}
	if len(res.Callees) == 0 {
		t.Fatal("Start has no callees, expected at least transform")
	}
	found := false
	for _, h := range res.Callees {
		if strings.Contains(h.Node.Name, "transform") {
			found = true
			if len(h.Callees) == 0 {
				t.Error("transform should have callees (emit)")
			}
		}
	}
	if !found {
		t.Errorf("transform not found in callees: %+v", res.Callees)
	}
}

func TestMCP_getConceptUnknownNameReturnsNoConceptError(t *testing.T) {
	t.Chdir(t.TempDir())
	// Store-based lookup: names that don't exist return a "no concept" error,
	// not an "invalid concept name" error (path-escape guard was removed).
	for _, name := range []string{"../secret", "a/b", `a\b`, "..", "x..y"} {
		_, _, err := handleGetConcept(t.Context(), nil, getConceptInput{Name: name})
		if err == nil || !strings.Contains(err.Error(), "no concept") {
			t.Errorf("handleGetConcept(%q) err = %v, want no-concept error", name, err)
		}
	}
}

func TestMCP_getImpactSurprisingOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	writeJavaTraitsFixture(t)

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

	text := callTool(t, session, "get_impact", map[string]any{"target": "Order", "depth": 2, "max_results": 50, "surprising_only": true})
	if !strings.Contains(text, "No surprising") {
		t.Errorf("get_impact surprising_only=true should report no surprising edges on the fixture:\n%s", text)
	}
}

func TestMCP_getFlowParityWithCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	writeGoFlowFixture(t)

	res, err := runGraphFlow("Start", 5, 10)
	if err != nil {
		t.Fatalf("runGraphFlow: %v", err)
	}
	cliJSON, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

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

	// The SDK serializes queryOutput{Result: "..."} into TextContent, so the
	// flow JSON arrives wrapped as {"result":"..."} — unwrap before comparing.
	out := callTool(t, session, "get_flow", map[string]any{"symbol": "Start", "depth": 5, "fan_out": 10})
	var wrapper struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil {
		t.Fatalf("unmarshal get_flow output: %v", err)
	}
	if wrapper.Result != string(cliJSON) {
		t.Errorf("get_flow and graph flow --json differ:\nMCP:\n%s\nCLI:\n%s", wrapper.Result, cliJSON)
	}
}

func TestGetBridgesToolRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["get_bridges"] {
		t.Error("newMCPServer does not serve get_bridges")
	}
}

// writeJavaCallFixture writes a Leaf->Mid->Base call chain (Mid bridges) and
// builds the graph, so get_bridges has a real chokepoint to surface.
func writeJavaCallFixture(t *testing.T) {
	t.Helper()
	files := map[string]string{
		"Base.java": "package p;\npublic class Base { public void run(){} }\n",
		"Mid.java":  "package p;\npublic class Mid { public void go(Base b){ b.run(); } }\n",
		"Leaf.java": "package p;\npublic class Leaf { public void start(Mid m){ m.go(null); } }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(name, []byte(src), 0o644); err != nil {
			t.Fatalf("writeJavaCallFixture: %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("writeJavaCallFixture: mkdir: %v", err)
	}
	st, err := store.Open(filepath.Join(".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("writeJavaCallFixture: store.Open: %v", err)
	}
	if _, err := extract.Build(".", extract.Extensions(), st); err != nil {
		t.Fatalf("writeJavaCallFixture: extract.Build: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("writeJavaCallFixture: store.Close: %v", err)
	}
}

// TestMCP_getBridgesRoundTrip exercises get_bridges end-to-end through the MCP
// server (spec §4), asserting the bridging node surfaces in the response.
func TestMCP_getBridgesRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	writeJavaCallFixture(t)

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

	out := callTool(t, session, "get_bridges", map[string]any{"top": 10})
	if !strings.Contains(out, "Bridge nodes") || !strings.Contains(out, "Mid") {
		t.Errorf("get_bridges should surface the Mid chokepoint:\n%s", out)
	}
}

// writeGoCycleFixture writes a two-symbol Go cycle (pkg/a.go calls B, pkg/b.go
// calls A) and builds the graph, so get_cycles has a real SCC to surface.
func writeGoCycleFixture(t *testing.T) {
	t.Helper()
	files := map[string]string{
		"pkg/a.go": "package pkg\nfunc A() { B() }\n",
		"pkg/b.go": "package pkg\nfunc B() { A() }\n",
	}
	for name, src := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatalf("writeGoCycleFixture: mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(name, []byte(src), 0o644); err != nil {
			t.Fatalf("writeGoCycleFixture: %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("writeGoCycleFixture: mkdir .tu-agent: %v", err)
	}
	st, err := store.Open(filepath.Join(".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("writeGoCycleFixture: store.Open: %v", err)
	}
	if _, err := extract.Build(".", extract.Extensions(), st); err != nil {
		t.Fatalf("writeGoCycleFixture: extract.Build: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("writeGoCycleFixture: store.Close: %v", err)
	}
}

// TestMCP_getCyclesRoundTrip exercises get_cycles end-to-end through the MCP
// server, asserting the mutual-recursion cycle surfaces in the response.
func TestMCP_getCyclesRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	writeGoCycleFixture(t)

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

	out := callTool(t, session, "get_cycles", map[string]any{})
	if !strings.Contains(out, "members") {
		t.Errorf("get_cycles should surface the A<->B cycle; got:\n%s", out)
	}
}

func TestMemRelationToolsRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	served := servedToolNames(t)
	for _, want := range []string{"mem_relate", "mem_related"} {
		if !served[want] {
			t.Errorf("newMCPServer does not serve %q", want)
		}
	}
}

func TestMCP_memRelateRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	// Seed an observation to relate from.
	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	obs, err := ms.Upsert("gotcha/cache", "cache is fragile", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_ = ms.Close()

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

	node := "internal/x.go::Foo"
	out := callTool(t, session, "mem_relate", map[string]any{"from": obs.ID, "to": node, "type": "related"})
	if !strings.Contains(out, "linked") {
		t.Errorf("mem_relate output unexpected: %q", out)
	}
	out = callTool(t, session, "mem_related", map[string]any{"node_id": node})
	if !strings.Contains(out, "gotcha/cache") {
		t.Errorf("mem_related should list the linked observation:\n%s", out)
	}
}

func TestMCP_memRelatedMarksStale(t *testing.T) {
	t.Chdir(t.TempDir())
	buildFixtureGraph(t) // gives the graph a real node so NodeCount() > 0.

	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	obs, err := ms.Upsert("gotcha/ghost", "linked to a node that no longer exists", memory.UpsertOpts{})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ghostNode := "widget.go::Ghost"
	if _, err := ms.Relate(obs.ID, ghostNode, "related"); err != nil {
		t.Fatalf("relate: %v", err)
	}
	_ = ms.Close()

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

	out := callTool(t, session, "mem_related", map[string]any{"node_id": ghostNode})
	if !strings.Contains(out, "gotcha/ghost") {
		t.Fatalf("mem_related should list the linked observation:\n%s", out)
	}
	if !strings.Contains(out, "possibly stale") {
		t.Errorf("mem_related must mark a note linked to a deleted node as stale:\n%s", out)
	}
}

func TestMemSessionToolsRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	served := servedToolNames(t)
	for _, want := range []string{"mem_session_start", "mem_session_end", "mem_session_list"} {
		if !served[want] {
			t.Errorf("newMCPServer does not serve %q", want)
		}
	}
}

func TestMCP_memSessionRoundTrip(t *testing.T) {
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

	out := callTool(t, session, "mem_session_start", map[string]any{})
	if !strings.Contains(out, "session started") {
		t.Errorf("mem_session_start output unexpected: %q", out)
	}
	out = callTool(t, session, "mem_session_end", map[string]any{"summary": "x"})
	if !strings.Contains(out, "session ended") {
		t.Errorf("mem_session_end output unexpected: %q", out)
	}
	out = callTool(t, session, "mem_session_start", map[string]any{})
	if !strings.Contains(out, "x") {
		t.Errorf("second mem_session_start should surface the previous summary %q:\n%s", "x", out)
	}
}

// TestHandleMemSessionEnd_PassesProjectThrough locks in the fix for
// mem_session_end dropping the project: it used to hardcode SessionEnd("",
// ...), so a session started with a project could never be found (and ended)
// by project. The stored session row must carry the project through to
// mem_session_list / the store.
func TestHandleMemSessionEnd_PassesProjectThrough(t *testing.T) {
	t.Chdir(t.TempDir())

	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionStart("acme"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	_, out, err := handleMemSessionEnd(context.Background(), nil,
		memSessionEndInput{Project: "acme", Summary: "wrapped up"})
	if err != nil {
		t.Fatalf("handleMemSessionEnd: %v", err)
	}
	if !strings.Contains(out.Result, "session ended") {
		t.Errorf("mem_session_end output unexpected: %q", out.Result)
	}

	s2, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	sessions, err := s2.SessionList("acme", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for project %q, got %d", "acme", len(sessions))
	}
	if sessions[0].Project != "acme" {
		t.Errorf("stored session project = %q, want %q", sessions[0].Project, "acme")
	}
	if sessions[0].EndedAt.IsZero() {
		t.Error("session should be ended")
	}
	if sessions[0].Summary != "wrapped up" {
		t.Errorf("summary = %q, want %q", sessions[0].Summary, "wrapped up")
	}
}

func TestMCPMemExportImport(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/mcp", "via mcp", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	if _, _, err := handleMemExport(context.Background(), nil, memExportMCPInput{}); err != nil {
		t.Fatalf("export: %v", err)
	}
	entries, _ := os.ReadDir(memoryChunksDir("."))
	if len(entries) != 1 {
		t.Fatalf("export must write one chunk, got %d", len(entries))
	}

	// Fresh DB in a second repo that shares the same chunk dir.
	dir2 := t.TempDir()
	if err := os.MkdirAll(memoryChunksDir(dir2), 0o755); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadDir(memoryChunksDir("."))
	for _, e := range src {
		b, _ := os.ReadFile(filepath.Join(memoryChunksDir("."), e.Name()))
		_ = os.WriteFile(filepath.Join(memoryChunksDir(dir2), e.Name()), b, 0o644)
	}
	if err := os.Chdir(dir2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := handleMemImport(context.Background(), nil, memImportMCPInput{}); err != nil {
		t.Fatalf("import: %v", err)
	}
	d, _ := memory.Open(memoryDBPath("."))
	defer d.Close()
	got, _, _ := d.Search("mcp", "", 0)
	if len(got) != 1 {
		t.Fatalf("imported observation not found: %+v", got)
	}
}

func TestMemSyncToolsRegistered(t *testing.T) {
	t.Chdir(t.TempDir())
	served := servedToolNames(t)
	for _, want := range []string{"mem_export", "mem_import"} {
		if !served[want] {
			t.Errorf("newMCPServer does not serve %q", want)
		}
	}
}

func TestMCPHasTestMutation(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["test_mutation"] {
		t.Error("newMCPServer does not serve test_mutation")
	}
}

func TestGetConceptFromStore(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".tu-agent", "graph"), 0o755)
	st, err := store.Open(filepath.Join(dir, ".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "widget rendering", Content: "---\nname: widgets\n---\nBODY"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	st.Close()

	t.Chdir(dir)

	// Single concept returns its content.
	_, out, err := handleGetConcept(t.Context(), nil, getConceptInput{Name: "widgets"})
	if err != nil {
		t.Fatalf("handleGetConcept: %v", err)
	}
	if !strings.Contains(out.Result, "BODY") {
		t.Errorf("content not returned: %q", out.Result)
	}
	// Empty name lists names + descriptions.
	_, list, err := handleGetConcept(t.Context(), nil, getConceptInput{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(list.Result, "widgets") || !strings.Contains(list.Result, "widget rendering") {
		t.Errorf("list missing concept: %q", list.Result)
	}
}

func TestGetConceptListCapped(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".tu-agent", "graph"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, ".tu-agent", "graph", "graph.db"), extract.ExtractorVersion)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const n = 60
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("concept%02d", i)
		if err := st.UpsertConcept(store.ConceptRow{
			Name:        name,
			Description: "desc " + name,
			Content:     "---\nname: " + name + "\n---\nBODY",
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	t.Chdir(dir)

	_, out, err := handleGetConcept(t.Context(), nil, getConceptInput{})
	if err != nil {
		t.Fatalf("handleGetConcept: %v", err)
	}
	if !strings.Contains(out.Result, "…and 10 more — pass a name to read one") {
		t.Errorf("expected '…and 10 more' marker, got:\n%s", out.Result)
	}
	listed := 0
	for _, l := range strings.Split(out.Result, "\n") {
		if strings.HasPrefix(l, "- ") {
			listed++
		}
	}
	if listed > 50 {
		t.Errorf("expected at most 50 listed concepts, got %d", listed)
	}
}

func TestFormatObservationsStaleMarker(t *testing.T) {
	obs := []memory.Observation{
		{ID: "a", TopicKey: "decision/x", Revision: 2, Content: "body-x"},
		{ID: "b", TopicKey: "decision/y", Revision: 1, Content: "body-y"},
	}
	out := formatObservations(obs, map[string]int{"a": 3})
	if !strings.Contains(out, "possibly stale: 3") {
		t.Errorf("missing stale marker for a:\n%s", out)
	}
	// The clean observation must NOT be flagged.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "decision/y") && strings.Contains(line, "stale") {
			t.Errorf("b should not be flagged: %q", line)
		}
	}
	// nil map → no markers at all.
	if strings.Contains(formatObservations(obs, nil), "stale") {
		t.Error("nil stale map should produce no markers")
	}
}

func TestHandleMemSavePersistsType(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, _, err := handleMemSave(context.Background(), nil, memSaveMCPInput{Topic: "trap/x", Content: "watch out", Type: "gotcha"}); err != nil {
		t.Fatalf("handleMemSave: %v", err)
	}
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	got, _, err := ms.Search("watch out", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "gotcha" {
		t.Fatalf("want one observation typed gotcha, got %+v", got)
	}
	if _, _, err := handleMemSave(context.Background(), nil, memSaveMCPInput{Topic: "trap/y", Content: "z", Type: "nonsense"}); err == nil {
		t.Error("handleMemSave with an invalid type should return the store's validation error")
	}
}
