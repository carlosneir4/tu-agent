package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func graphDBPath(root string) string {
	return filepath.Join(root, ".tu-agent", "graph.db")
}

func openGraphStore() (*store.Store, error) {
	root := repoRoot()
	if err := os.MkdirAll(filepath.Join(root, ".tu-agent"), 0o755); err != nil {
		return nil, fmt.Errorf("creating .tu-agent dir: %w", err)
	}
	return store.Open(graphDBPath(root), extract.ExtractorVersion)
}

func runGraphBuild(subpath string) error {
	return runGraphBuildQuiet(subpath, false)
}

// runGraphBuildQuiet builds/updates the graph. In quiet mode it is safe to run
// on every edit from a global hook: it no-ops when no graph exists, prints no
// success output, and does not rewrite .mcp.json. Errors still surface.
func runGraphBuildQuiet(subpath string, quiet bool) (err error) {
	// No-op guard — must run BEFORE openGraphStore (which MkdirAll's .tu-agent
	// and creates the DB). The global plugin hook fires in every repo, including
	// those that never ran tu-agent; do not bootstrap an unwanted graph there.
	if quiet {
		if _, statErr := os.Stat(graphDBPath(repoRoot())); errors.Is(statErr, fs.ErrNotExist) {
			return nil
		} else if statErr != nil {
			return fmt.Errorf("graph update: stat graph db: %w", statErr)
		}
	}
	start := time.Now()
	defer func() {
		// Only quiet mode is a hook invocation (graph update --quiet / --post-bash
		// / --quiet --announce all pass quiet=true). A manual `graph update`
		// (quiet=false) must NOT emit an Event=hook row on failure — that would
		// pollute the hook failure-rate and p50/p95 in `stats --insights` with a
		// non-hook event, symmetric with the memory hook commands gating on their
		// hook-mode flag.
		if err != nil && quiet {
			recordHook("graph update", time.Since(start), err)
		}
	}()
	s, err := openGraphStore()
	if err != nil {
		return err
	}
	defer s.Close()
	root, err := filepath.Abs(repoRoot())
	if err != nil {
		return fmt.Errorf("graph build: resolving repo root: %w", err)
	}
	scope := ""
	if subpath != "" {
		abs, err := filepath.Abs(subpath)
		if err != nil {
			return fmt.Errorf("graph build: resolving %s: %w", subpath, err)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("graph build: %s is outside the repository root %s", subpath, root)
		}
		if rel != "." {
			scope = filepath.ToSlash(rel)
		}
	}
	res, err := extract.BuildScoped(root, scope, extract.Extensions(), s)
	if err != nil {
		return err
	}
	// A successful build records the graph_refresh row here. A later
	// NodeCount/FileCount error (below) cannot also emit a hook-failure row for
	// the same invocation: those calls only run in the !quiet branch, and the
	// hook-failure emission above is gated on quiet — the two are mutually
	// exclusive, so there is no double-count.
	recordGraphRefresh(res, time.Since(start))
	if quiet {
		return nil
	}
	fmt.Printf("graph: parsed=%d unchanged=%d deleted=%d failed=%d\n",
		res.Parsed, res.Unchanged, res.Deleted, res.Failed)
	nodes, err := s.NodeCount()
	if err != nil {
		return fmt.Errorf("graph update: node count: %w", err)
	}
	files, err := s.FileCount()
	if err != nil {
		return fmt.Errorf("graph update: file count: %w", err)
	}
	if w := graphEmptyWarning(nodes, files); w != "" {
		fmt.Fprint(os.Stderr, w)
	}
	// Off by default: the plugin (marketplace) already provides the tu-agent-graph
	// MCP server via its shim, which auto-updates. Writing .mcp.json here would
	// add a duplicate server pinned to this binary's path (no auto-update). CLI-only
	// users without the plugin opt in with --write-mcp.
	if writeMCPOptIn {
		if err := writeMCPConfig(repoRoot()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write .mcp.json: %v\n", err)
		}
	}
	return nil
}

// writeMCPOptIn gates writing a repo-local .mcp.json during a graph build. The
// `graph build --write-mcp` flag sets it; default off (the plugin provides the
// MCP server).
var writeMCPOptIn bool

func writeMCPConfig(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	type mcpServer struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	cfg := map[string]map[string]mcpServer{
		"mcpServers": {
			"tu-agent-graph": {Command: exe, Args: []string{"mcp"}},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".mcp.json"), append(data, '\n'), 0o644)
}

func loadQueryGraph() (*query.Graph, error) {
	s, err := openGraphStore()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	nodes, err := s.AllNodes()
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("graph is empty — run 'tu-agent graph build' first")
	}
	edges, err := s.AllEdges()
	if err != nil {
		return nil, err
	}
	files, err := s.Files()
	if err != nil {
		return nil, fmt.Errorf("loadQueryGraph: read file metadata: %w", err)
	}
	pkgByPath := make(map[string]string, len(files))
	for path, f := range files {
		if f.Package != "" {
			pkgByPath[path] = f.Package
		}
	}
	return query.NewGraph(nodes, edges, query.WithPackages(pkgByPath)), nil
}

// resolveTargetChecked resolves target to a node ID. It errors when nothing in
// the graph matches (with up to 3 candidate suggestions when a substring search
// finds near-misses), and returns a one-line disclosure when the resolution was
// not an exact name match, so callers can show how the target was interpreted.
func resolveTargetChecked(g *query.Graph, target string) (id, disclosure string, err error) {
	// A literal, already-known graph node ID (e.g. "path/File.java::Symbol", as
	// produced by other graph output) resolves directly — it is not an ambiguous
	// human-typed name, so no disclosure is warranted.
	if n, ok := g.NodeByID(target); ok {
		return n.ID, "", nil
	}
	hits := g.Find(target)
	if len(hits) == 0 {
		return "", "", fmt.Errorf("symbol not found: %q — use find_symbol to locate the right name", target)
	}
	if len(hits) == 1 {
		n := hits[0]
		if strings.EqualFold(n.Name, target) {
			return n.ID, "", nil
		}
		return n.ID, fmt.Sprintf("resolved %q → %s (1 candidate)", target, n.ID), nil
	}
	lower := strings.ToLower(target)
	var exactClass, exactAny string
	for _, n := range hits {
		if strings.ToLower(n.Name) != lower {
			continue
		}
		if exactAny == "" {
			exactAny = n.ID
		}
		if n.Kind == graph.KindClass && exactClass == "" {
			exactClass = n.ID
		}
	}
	id = exactClass
	if id == "" {
		id = exactAny
	}
	if id == "" {
		names := make([]string, 0, 3)
		for _, n := range hits {
			names = append(names, n.Name)
			if len(names) == 3 {
				break
			}
		}
		return "", "", fmt.Errorf("symbol not found: %q — did you mean: %s (use find_symbol to disambiguate)", target, strings.Join(names, ", "))
	}
	return id, fmt.Sprintf("resolved %q → %s (%d candidates — use find_symbol to disambiguate)", target, id, len(hits)), nil
}

func runGraphImpact(target string, depth, maxResults int, cfg query.SurpriseConfig, surprisingOnly bool) (string, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", err
	}
	targetID, disclosure, err := resolveTargetChecked(g, target)
	if err != nil {
		return "", err
	}
	result, err := g.Impact(targetID, depth, query.DirUp, 0)
	if err != nil {
		return "", err
	}
	result.SurprisingEdges = g.ComputeSurprising(targetID, result, cfg)
	if surprisingOnly {
		out := query.FormatSurprising(targetID, result, maxResults)
		if disclosure != "" {
			out = disclosure + "\n\n" + out
		}
		return out, nil
	}
	formatted := query.FormatImpact(targetID, result, maxResults)
	note := ""
	if core, ok := g.CyclicCoreOf(targetID); ok {
		coreSet := make(map[string]bool, len(core))
		for _, m := range core {
			coreSet[m] = true
		}
		mates := 0
		for _, id := range result.NodeIDs() {
			if coreSet[id] {
				mates++
			}
		}
		note = fmt.Sprintf("\n_(%d of these dependents are cycle-mates (co-coupled with `%s`); the rest are genuine downstream. The cyclic core has %d members.)_\n",
			mates, targetID, len(core))
	}
	out := formatted + note + relatedKnowledgeSection(targetID, result.NodeIDs())
	if disclosure != "" {
		out = disclosure + "\n\n" + out
	}
	return out, nil
}

// relatedKnowledgeSection returns a "Related knowledge" markdown block listing
// observations linked (via memory_relations) to the target or any affected node.
// Best-effort: a missing memory store yields an empty string, never an error.
func relatedKnowledgeSection(targetID string, affectedIDs []string) string {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return ""
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("related knowledge: store close failed", "err", cerr)
		}
	}()
	ids := append([]string{targetID}, affectedIDs...)
	rels, err := s.RelationsTo(ids)
	if err != nil || len(rels) == 0 {
		return ""
	}
	// Per observation: track whether it is linked ONLY via auto relations.
	// Curated wins: if any relation for that obs is non-auto, mark it false.
	autoOnly := make(map[string]bool)
	for _, r := range rels {
		if r.Type == "documents_auto" {
			if _, seen := autoOnly[r.FromID]; !seen {
				autoOnly[r.FromID] = true
			}
		} else {
			autoOnly[r.FromID] = false
		}
	}
	obsIDs := make([]string, 0, len(autoOnly))
	for id := range autoOnly {
		obsIDs = append(obsIDs, id)
	}
	obs, err := s.ObservationsByID(obsIDs)
	if err != nil || len(obs) == 0 {
		return ""
	}
	stale := recallStale(s, obs)
	var b strings.Builder
	b.WriteString("\n## Related knowledge\n")
	for _, o := range obs {
		key := o.TopicKey
		if key == "" {
			key = o.Title
		}
		marker := ""
		if autoOnly[o.ID] {
			marker = " ~auto"
		}
		if n := stale[o.ID]; n > 0 {
			marker += fmt.Sprintf(" ⚠ possibly stale: %d linked symbol(s) no longer in the graph — verify before trusting", n)
		}
		fmt.Fprintf(&b, "- [%s] %s (rev %d)%s\n", key, firstLine(o.Content, 80), o.Revision, marker)
	}
	return b.String()
}

func runGraphContext(target string, depth, maxResults int) (string, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", err
	}
	targetID, disclosure, err := resolveTargetChecked(g, target)
	if err != nil {
		return "", err
	}
	res, err := g.Context(targetID, depth)
	if err != nil {
		return "", err
	}
	out := query.FormatContext(res, maxResults)
	if disclosure != "" {
		out = disclosure + "\n\n" + out
	}
	return out, nil
}

// runGraphTraits is the single assembly path shared by the CLI subcommand and
// the get_traits MCP handler, so both emit identical JSON.
func runGraphTraits(target string, depth, maxResults int) (*query.TraitsResult, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return nil, err
	}
	targetID, _, err := resolveTargetChecked(g, target)
	if err != nil {
		return nil, err
	}
	return g.Traits(targetID, depth, maxResults)
}

// runGraphFlow is the single assembly path shared by the CLI subcommand and
// the get_flow MCP handler, so both emit identical JSON.
func runGraphFlow(target string, depth, fanOut int) (*query.FlowResult, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return nil, err
	}
	targetID, _, err := resolveTargetChecked(g, target)
	if err != nil {
		return nil, err
	}
	return g.Flow(targetID, depth, fanOut)
}

func runGraphFind(symbol string) (string, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", err
	}
	return query.FormatFind(symbol, g.Find(symbol)), nil
}

func runGraphStatus() (string, error) {
	s, err := openGraphStore()
	if err != nil {
		return "", err
	}
	defer s.Close()
	nodes, err := s.AllNodes()
	if err != nil {
		return "", err
	}
	edges, err := s.AllEdges()
	if err != nil {
		return "", err
	}
	files, err := s.Files()
	if err != nil {
		return "", err
	}
	failed := 0
	for _, f := range files {
		if f.Status == "failed" {
			failed++
		}
	}
	ev, err := s.Meta("extractor_version")
	if err != nil {
		return "", fmt.Errorf("runGraphStatus: reading extractor version: %w", err)
	}
	return fmt.Sprintf("files=%d (failed=%d) nodes=%d edges=%d extractor=%s\n",
		len(files), failed, len(nodes), len(edges), ev), nil
}

var (
	graphDepth             int
	graphMaxResults        int
	graphSurprisingOnly    bool
	graphDomainDepth       int
	graphSurpriseThreshold float64
	graphMinDomainEdges    int
	graphContextDepth      int
	graphContextMax        int
	graphTraitsDepth       int
	graphTraitsMax         int
	graphTraitsJSON        bool
	graphFlowDepth         int
	graphFlowFanOut        int
	graphFlowJSON          bool
	graphFlowMermaid       bool
	graphQuiet             bool
	graphPostBash          bool

	graphBridgesTop     int
	graphBridgesSamples int
	graphBridgesJSON    bool
)

var graphFlowCmd = &cobra.Command{
	Use:   "flow <symbol>",
	Short: "Trace the execution call tree from a symbol with boundary and dispatch annotations",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runGraphFlow(args[0], graphFlowDepth, graphFlowFanOut)
		if err != nil {
			return err
		}
		switch {
		case graphFlowJSON:
			data, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return fmt.Errorf("graph flow: marshal: %w", err)
			}
			fmt.Println(string(data))
		case graphFlowMermaid:
			fmt.Print(query.FormatFlowMermaid(res))
		default:
			fmt.Print(query.FormatFlow(res))
		}
		return nil
	},
}

var graphTraitsCmd = &cobra.Command{
	Use:   "traits <symbol>",
	Short: "Trait view: shared interfaces, where the logic lives, and the blast radius",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runGraphTraits(args[0], graphTraitsDepth, graphTraitsMax)
		if err != nil {
			return err
		}
		if graphTraitsJSON {
			data, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return fmt.Errorf("graph traits: marshal: %w", err)
			}
			fmt.Println(string(data))
			return nil
		}
		fmt.Print(query.FormatTraits(res))
		return nil
	},
}

func runGraphBridges(top, samples int, jsonOut bool) (string, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", err
	}
	ranks := g.BridgeTop(query.BridgeConfig{Samples: samples}, top)
	if jsonOut {
		data, err := json.MarshalIndent(ranks, "", "  ")
		if err != nil {
			return "", fmt.Errorf("graph bridges: marshal: %w", err)
		}
		return string(data) + "\n", nil
	}
	if len(ranks) == 0 {
		return "No chokepoints detected.\n", nil
	}
	var sb strings.Builder
	sb.WriteString("## Bridge nodes (chokepoints)\n\n")
	for _, r := range ranks {
		loc := r.Path
		if loc == "" {
			loc = r.ID
		}
		fmt.Fprintf(&sb, "- `%s` (%s)  [score %.0f]\n", r.Name, loc, r.Score)
	}
	return sb.String(), nil
}

func runGraphCycles(jsonOut bool) (string, error) {
	g, err := loadQueryGraph()
	if err != nil {
		return "", err
	}
	cycles := make([][]string, 0)
	for _, comp := range g.StronglyConnectedComponents() {
		if len(comp) > 1 {
			cycles = append(cycles, comp)
		}
	}
	if jsonOut {
		data, err := json.MarshalIndent(cycles, "", "  ")
		if err != nil {
			return "", fmt.Errorf("graph cycles: marshal: %w", err)
		}
		return string(data) + "\n", nil
	}
	if len(cycles) == 0 {
		return "No dependency cycles found.\n", nil
	}
	const cyclesMemberCap = 20
	var sb strings.Builder
	sb.WriteString("## Dependency cycles (strongly-connected components, size > 1)\n\n")
	for i, comp := range cycles {
		members := comp
		extra := 0
		if len(members) > cyclesMemberCap {
			extra = len(members) - cyclesMemberCap
			members = members[:cyclesMemberCap]
		}
		fmt.Fprintf(&sb, "%d. %d members: %s", i+1, len(comp), strings.Join(members, ", "))
		if extra > 0 {
			fmt.Fprintf(&sb, " (+%d more)", extra)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nCut these to break coupling; changes to any member ripple across the whole component.\n")
	return sb.String(), nil
}

var graphCyclesJSON bool

var graphCyclesCmd = &cobra.Command{
	Use:   "cycles",
	Short: "List dependency cycles (strongly-connected components, size > 1)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := runGraphCycles(graphCyclesJSON)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), out)
		return nil
	},
}

var graphBridgesCmd = &cobra.Command{
	Use:   "bridges",
	Short: "List architectural chokepoints (high betweenness over the call graph)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := runGraphBridges(graphBridgesTop, graphBridgesSamples, graphBridgesJSON)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), out)
		return nil
	},
}

var graphCmd = &cobra.Command{
	GroupID: "graph",
	Use:     "graph",
	Short:   "Build and query the code knowledge graph",
}

var graphBuildCmd = &cobra.Command{
	Use:   "build [path]",
	Short: "Build or incrementally refresh the graph",
	Long:  "Build or incrementally refresh the graph. A path argument restricts the refresh to that subtree; entries outside it are never deleted.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := ""
		if len(args) == 1 {
			sub = args[0]
		}
		return runGraphBuild(sub)
	},
}

// graphAnnounce gates printing a session-orientation nudge after `graph
// update`. SessionStart hooks use it: Claude Code injects the hook's stdout
// into the agent's context, so the nudge reaches the model every session.
var graphAnnounce bool

// announceGraph prints the orientation nudge to stdout. Silent no-op when no
// graph exists (the hook fires in every repo, including ones that never ran
// tu-agent).
func announceGraph() error {
	if _, err := os.Stat(graphDBPath(repoRoot())); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("graph announce: stat graph db: %w", err)
	}
	s, err := openGraphStore()
	if err != nil {
		return err
	}
	defer s.Close()
	n, err := s.NodeCount()
	if err != nil {
		return fmt.Errorf("graph announce: %w", err)
	}
	files, err := s.FileCount()
	if err != nil {
		return fmt.Errorf("graph announce: %w", err)
	}
	// A graph with file records but zero nodes is broken, not ready: get_context
	// and find_symbol would silently return nothing. Surface it loudly in the
	// SessionStart context instead of a reassuring "graph ready (0 nodes)".
	if w := graphEmptyWarning(n, files); w != "" {
		fmt.Print(w)
		return nil
	}
	fmt.Printf(`tu-agent: graph ready (%d nodes). Follow the PROTOCOL in CLAUDE.md: query get_context BEFORE editing any file (also get_impact, find_symbol, get_concept), and call mem_recent now to recall prior decisions.
NOTE: the tu-agent-graph MCP tools may be DEFERRED — absent from your active tool list. If so, load them with your tool-search mechanism (e.g. ToolSearch query "tu-agent-graph") instead of concluding they are unavailable. CLI fallback: tu-agent graph context <file-or-symbol>.
`, n)
	return nil
}

// graphEmptyWarning returns a loud warning when the graph holds file records but
// zero nodes — the silent-empty-graph state (a wiped node store, or every file
// failing to parse). It returns "" when the graph is healthy or genuinely
// unbuilt (no files at all), so callers can print it unconditionally. Without
// this the only signal is a benign-looking "parsed=0 unchanged=N" line, so a
// broken graph goes unnoticed until someone inspects it.
func graphEmptyWarning(nodes, files int) string {
	if nodes == 0 && files > 0 {
		return fmt.Sprintf("⚠ tu-agent: graph is EMPTY (%d files, 0 nodes) — get_context/find_symbol will return NOTHING. Run `tu-agent learn` to rebuild the graph.\n", files)
	}
	return ""
}

var graphUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Re-parse only changed files (alias for build)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphPostBash {
			return postBashDecision(cmd.InOrStdin(), func() error { return runGraphBuildQuiet("", true) })
		}
		if err := runGraphBuildQuiet("", graphQuiet); err != nil {
			return err
		}
		if graphAnnounce {
			return announceGraph()
		}
		return nil
	},
}

var graphImpactCmd = &cobra.Command{
	Use:   "impact <target>",
	Short: "Who is affected if you change <target>",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runGraphImpact(args[0], graphDepth, graphMaxResults, query.SurpriseConfig{
			DomainDepth:    graphDomainDepth,
			Threshold:      graphSurpriseThreshold,
			MinDomainEdges: graphMinDomainEdges,
		}, graphSurprisingOnly)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

var graphFindCmd = &cobra.Command{
	Use:   "find <symbol>",
	Short: "Where a symbol is defined",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runGraphFind(args[0])
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

var graphContextCmd = &cobra.Command{
	Use:   "context <target>",
	Short: "Everything relevant to touching <target>: blast radius, skills, conventions, tests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runGraphContext(args[0], graphContextDepth, graphContextMax)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

var graphStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Graph size, failed files, and extractor version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runGraphStatus()
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

func init() {
	graphBuildCmd.Flags().BoolVar(&writeMCPOptIn, "write-mcp", false,
		"write a repo-local .mcp.json registering tu-agent-graph (for CLI-only use without the plugin; off by default since the plugin already provides the server)")
	graphImpactCmd.Flags().IntVar(&graphDepth, "depth", 2, "BFS depth")
	graphImpactCmd.Flags().IntVar(&graphMaxResults, "max-results", 50, "cap on returned nodes")
	graphImpactCmd.Flags().BoolVar(&graphSurprisingOnly, "surprising-only", false, "print only the surprising cross-domain dependencies section")
	graphImpactCmd.Flags().IntVar(&graphDomainDepth, "domain-depth", 2, "package/path segments that define a domain for surprise scoring")
	graphImpactCmd.Flags().Float64Var(&graphSurpriseThreshold, "surprise-threshold", 0.10, "cross-domain pair share below which a dependency is surprising")
	graphImpactCmd.Flags().IntVar(&graphMinDomainEdges, "min-domain-edges", 5, "min cross-domain out-edges a source domain needs before any of its edges can be surprising")
	graphContextCmd.Flags().IntVar(&graphContextDepth, "depth", 2, "BFS depth for the blast radius")
	graphContextCmd.Flags().IntVar(&graphContextMax, "max-results", 50, "cap on returned nodes")
	graphTraitsCmd.Flags().IntVar(&graphTraitsDepth, "depth", 2, "BFS depth for the impact set")
	graphTraitsCmd.Flags().IntVar(&graphTraitsMax, "max-results", 50, "cap on impact nodes")
	graphTraitsCmd.Flags().BoolVar(&graphTraitsJSON, "json", false, "emit the trait view as JSON")
	graphFlowCmd.Flags().IntVar(&graphFlowDepth, "depth", 5, "call tree depth")
	graphFlowCmd.Flags().IntVar(&graphFlowFanOut, "fan-out", 10, "maximum direct callees per node (0 = unlimited)")
	graphFlowCmd.Flags().BoolVar(&graphFlowJSON, "json", false, "emit the flow tree as JSON")
	graphFlowCmd.Flags().BoolVar(&graphFlowMermaid, "mermaid", false, "emit a Mermaid flowchart diagram")
	graphUpdateCmd.Flags().BoolVar(&graphQuiet, "quiet", false,
		"suppress success output and skip .mcp.json rewrite; no-op if no graph exists (for hooks)")
	graphUpdateCmd.Flags().BoolVar(&graphPostBash, "post-bash", false,
		"read a PostToolUse payload on stdin; reconcile only if the command mutated the tree (implies --quiet)")
	graphUpdateCmd.Flags().BoolVar(&graphAnnounce, "announce", false,
		"after updating, print a session-orientation nudge for the agent (for SessionStart hooks; silent when no graph exists)")
	graphBridgesCmd.Flags().IntVar(&graphBridgesTop, "top", 20, "number of chokepoints to list")
	graphBridgesCmd.Flags().IntVar(&graphBridgesSamples, "samples", 100, "source nodes sampled for betweenness")
	graphBridgesCmd.Flags().BoolVar(&graphBridgesJSON, "json", false, "emit JSON")
	graphCyclesCmd.Flags().BoolVar(&graphCyclesJSON, "json", false, "emit JSON")
	graphCmd.AddCommand(graphBuildCmd, graphUpdateCmd, graphImpactCmd, graphContextCmd, graphFindCmd, graphStatusCmd, graphTraitsCmd, graphFlowCmd, graphBridgesCmd, graphCyclesCmd)
	rootCmd.AddCommand(graphCmd)
}
