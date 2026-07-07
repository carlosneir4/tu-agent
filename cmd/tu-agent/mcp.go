package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/graph/extract"
	"github.com/tu/tu-agent/internal/graph/query"
	"github.com/tu/tu-agent/internal/memory"
)

// impactInput holds the parameters for the get_impact tool.
type impactInput struct {
	Target         string `json:"target"     jsonschema:"the symbol or node ID to analyse"`
	Depth          int    `json:"depth"      jsonschema:"BFS depth (default 2)"`
	MaxResults     int    `json:"max_results" jsonschema:"maximum nodes to return (default 50)"`
	SurprisingOnly bool   `json:"surprising_only" jsonschema:"return only the surprising cross-domain dependencies (optional)"`
}

// contextInput holds the parameters for the get_context tool.
type contextInput struct {
	Target     string `json:"target"     jsonschema:"the symbol or node ID to get context for"`
	Depth      int    `json:"depth"      jsonschema:"BFS depth (default 2)"`
	MaxResults int    `json:"max_results" jsonschema:"maximum nodes to return (default 50)"`
}

// findInput holds the parameters for the find_symbol tool.
type findInput struct {
	Symbol string `json:"symbol" jsonschema:"symbol name to search for"`
}

// memSaveMCPInput holds the parameters for the mem_save tool.
type memSaveMCPInput struct {
	Topic   string `json:"topic"   jsonschema:"topic key for upsert (slash/separated, e.g. architecture/auth)"`
	Content string `json:"content" jsonschema:"the observation to save"`
	Scope   string `json:"scope,omitempty" jsonschema:"optional scope: 'project' (default, shared with the team via the committed chunk) or 'personal' (kept local, never exported)"`
	Type    string `json:"type,omitempty" jsonschema:"optional observation type: bug-pattern | decision | architecture | testing | reference | gotcha | skill"`
}

// memSearchMCPInput holds the parameters for the mem_search tool.
type memSearchMCPInput struct {
	Query string `json:"query" jsonschema:"keyword to search in memory"`
	Type  string `json:"type,omitempty" jsonschema:"restrict to one observation type: bug-pattern | decision | architecture | testing | reference | gotcha | skill"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 20; raise for more)"`
}

// memRecentMCPInput holds the parameters for the mem_recent tool.
type memRecentMCPInput struct {
	N int `json:"n" jsonschema:"number of recent observations (default 5)"`
}

// memClustersMCPInput holds the parameters for the mem_clusters tool.
type memClustersMCPInput struct {
	Min int `json:"min,omitempty" jsonschema:"minimum notes for a cluster to be suggested (default 5)"`
}

// crystallizeSaveMCPInput holds the parameters for the crystallize_save tool.
type crystallizeSaveMCPInput struct {
	Label string `json:"label"         jsonschema:"the cluster label to crystallize (from mem_clusters)"`
	Body  string `json:"body"          jsonschema:"the generated SKILL.md body (frontmatter + standard; no provenance marker)"`
	Min   int    `json:"min,omitempty" jsonschema:"minimum cluster size (default 5; pass 3 to match mem_clusters min:3)"`
}

// memExportMCPInput is the (empty) parameter set for mem_export.
type memExportMCPInput struct{}

// memImportMCPInput is the (empty) parameter set for mem_import.
type memImportMCPInput struct{}

// memConflictsMCPInput is the (empty) parameter set for mem_conflicts.
type memConflictsMCPInput struct{}

// memRelateInput holds the parameters for the mem_relate tool.
type memRelateInput struct {
	From string `json:"from" jsonschema:"source id (observation ID or graph node ID)"`
	To   string `json:"to"   jsonschema:"target id (observation ID or graph node ID)"`
	Type string `json:"type" jsonschema:"relation type: related|supersedes|documents|conflicts_with (default related)"`
}

// memRelatedInput holds the parameters for the mem_related tool.
type memRelatedInput struct {
	NodeID string `json:"node_id" jsonschema:"graph node ID to find related observations for"`
}

// memSessionStartInput holds the parameters for the mem_session_start tool.
type memSessionStartInput struct {
	Project string `json:"project,omitempty" jsonschema:"session project label (optional)"`
}

// memSessionEndInput holds the parameters for the mem_session_end tool.
type memSessionEndInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (same value passed to mem_session_start)"`
	Summary string `json:"summary,omitempty" jsonschema:"explicit summary (composed from observations if omitted)"`
}

// memSessionListInput holds the parameters for the mem_session_list tool.
type memSessionListInput struct {
	N int `json:"n,omitempty" jsonschema:"number of sessions to list (default 10)"`
}

// memRescopeMCPInput holds the parameters for the mem_rescope tool.
type memRescopeMCPInput struct {
	Topic     string `json:"topic"                jsonschema:"topic key of the observation to rescope"`
	Scope     string `json:"scope"                jsonschema:"target scope, e.g. 'personal' (kept local) or 'project' (shared)"`
	FromScope string `json:"from_scope,omitempty" jsonschema:"current scope to move from (default 'project')"`
}

// memDeleteMCPInput holds the parameters for the mem_delete tool.
type memDeleteMCPInput struct {
	Topic string `json:"topic"           jsonschema:"topic key of the observation to delete"`
	Scope string `json:"scope,omitempty" jsonschema:"scope of the observation (default 'project')"`
}

// orDefault returns v, or fallback when v is empty.
func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// queryOutput is the structured output returned by all three tools.
type queryOutput struct {
	Result string `json:"result"`
}

func handleImpact(_ context.Context, _ *mcp.CallToolRequest, in impactInput) (*mcp.CallToolResult, queryOutput, error) {
	depth := in.Depth
	if depth <= 0 {
		depth = 2
	}
	max := in.MaxResults
	if max <= 0 {
		max = 50
	}
	out, err := runGraphImpact(in.Target, depth, max, query.SurpriseConfig{}, in.SurprisingOnly)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

func handleContext(_ context.Context, _ *mcp.CallToolRequest, in contextInput) (*mcp.CallToolResult, queryOutput, error) {
	depth := in.Depth
	if depth <= 0 {
		depth = 2
	}
	max := in.MaxResults
	if max <= 0 {
		max = 50
	}
	out, err := runGraphContext(in.Target, depth, max)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

func handleFind(_ context.Context, _ *mcp.CallToolRequest, in findInput) (*mcp.CallToolResult, queryOutput, error) {
	out, err := runGraphFind(in.Symbol)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

func handleMemSave(_ context.Context, _ *mcp.CallToolRequest, in memSaveMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Topic == "" || in.Content == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_save: topic and content are required")
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_save: store close failed", "err", cerr)
		}
	}()
	obs, err := s.Upsert(in.Topic, in.Content, memory.UpsertOpts{Scope: in.Scope, Type: in.Type, Source: "mcp", Author: gitAuthor()})
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: fmt.Sprintf("saved %s rev:%d", obs.TopicKey, obs.Revision)}, nil
}

func handleMemSearch(_ context.Context, _ *mcp.CallToolRequest, in memSearchMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Query == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_search: query is required")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_search: store close failed", "err", cerr)
		}
	}()
	obs, total, err := s.Search(in.Query, in.Type, limit)
	if err != nil {
		return nil, queryOutput{}, err
	}
	result := formatObservations(obs, recallStale(s, obs))
	if total > len(obs) {
		result += fmt.Sprintf("\n\nshowing %d of %d — refine the query or raise limit", len(obs), total)
	}
	return nil, queryOutput{Result: result}, nil
}

func handleMemRecent(_ context.Context, _ *mcp.CallToolRequest, in memRecentMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	n := in.N
	if n <= 0 {
		n = 5
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_recent: store close failed", "err", cerr)
		}
	}()
	obs, err := s.Recent(n)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: formatObservations(obs, recallStale(s, obs))}, nil
}

func handleMemClusters(_ context.Context, _ *mcp.CallToolRequest, in memClustersMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	min := in.Min
	if min <= 0 {
		min = 5
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_clusters: store close failed", "err", cerr)
		}
	}()
	obs, err := s.List()
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: crystallize.Format(crystallize.Detect(obs, min))}, nil
}

func handleMemConflicts(_ context.Context, _ *mcp.CallToolRequest, _ memConflictsMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_conflicts: store close failed", "err", cerr)
		}
	}()
	rels, err := s.RelationsByType("conflicts_with")
	if err != nil {
		return nil, queryOutput{}, err
	}
	byID, err := conflictTopicMap(s, rels)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: formatConflicts(rels, byID)}, nil
}

func handleCrystallizeSave(_ context.Context, _ *mcp.CallToolRequest, in crystallizeSaveMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Label == "" || in.Body == "" {
		return nil, queryOutput{}, fmt.Errorf("crystallize_save: label and body are required")
	}
	path, err := saveCrystallizedSkill(in.Label, in.Body, in.Min)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: "crystallized " + in.Label + " -> " + path}, nil
}

func handleMemExport(_ context.Context, _ *mcp.CallToolRequest, _ memExportMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_export: store close failed", "err", cerr)
		}
	}()
	author := gitAuthor()
	recs, err := s.ExportRecords(author)
	if err != nil {
		return nil, queryOutput{}, err
	}
	path, written, err := memory.WriteChunk(memoryChunksDir(repoRoot()), author, recs)
	if err != nil {
		return nil, queryOutput{}, err
	}
	status := "unchanged"
	if written {
		status = "wrote"
	}
	return nil, queryOutput{Result: fmt.Sprintf("%s %s (%d observations)", status, path, len(recs))}, nil
}

func handleMemImport(_ context.Context, _ *mcp.CallToolRequest, _ memImportMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	recs, err := memory.ReadAllChunks(memoryChunksDir(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_import: store close failed", "err", cerr)
		}
	}()
	res, err := s.ImportRecords(recs)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: fmt.Sprintf("imported: %d new, %d updated, %d unchanged",
		res.Inserted, res.Updated, res.Skipped)}, nil
}

func handleMemRelate(_ context.Context, _ *mcp.CallToolRequest, in memRelateInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.From == "" || in.To == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_relate: from and to are required")
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_relate: store close failed", "err", cerr)
		}
	}()
	rel, err := s.Relate(in.From, in.To, in.Type)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: fmt.Sprintf("linked %s --%s--> %s", rel.FromID, rel.Type, rel.ToID)}, nil
}

func handleMemRelated(_ context.Context, _ *mcp.CallToolRequest, in memRelatedInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.NodeID == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_related: node_id is required")
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_related: store close failed", "err", cerr)
		}
	}()
	rels, err := s.RelationsTo([]string{in.NodeID})
	if err != nil {
		return nil, queryOutput{}, err
	}
	ids := make([]string, 0, len(rels))
	for _, r := range rels {
		ids = append(ids, r.FromID)
	}
	obs, err := s.ObservationsByID(ids)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: formatObservations(obs, recallStale(s, obs))}, nil
}

func handleMemSessionStart(_ context.Context, _ *mcp.CallToolRequest, in memSessionStartInput) (*mcp.CallToolResult, queryOutput, error) {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_session_start: store close failed", "err", cerr)
		}
	}()
	sess, prev, err := s.SessionStart(in.Project)
	if err != nil {
		return nil, queryOutput{}, err
	}
	res := fmt.Sprintf("session started: %s", sess.ID)
	if prev != "" {
		res += "\n\n## Last session\n" + prev
	}
	return nil, queryOutput{Result: res}, nil
}

func handleMemSessionEnd(_ context.Context, _ *mcp.CallToolRequest, in memSessionEndInput) (*mcp.CallToolResult, queryOutput, error) {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_session_end: store close failed", "err", cerr)
		}
	}()
	sess, err := s.SessionEnd(in.Project, in.Summary)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: "session ended: " + sess.ID + "\nsummary: " + sess.Summary}, nil
}

func handleMemSessionList(_ context.Context, _ *mcp.CallToolRequest, in memSessionListInput) (*mcp.CallToolResult, queryOutput, error) {
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_session_list: store close failed", "err", cerr)
		}
	}()
	list, err := s.SessionList("", in.N)
	if err != nil {
		return nil, queryOutput{}, err
	}
	var sb strings.Builder
	if len(list) == 0 {
		sb.WriteString("no sessions")
	}
	for _, ss := range list {
		state := "active"
		if !ss.EndedAt.IsZero() {
			state = ss.EndedAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&sb, "%s  %s  %s\n", ss.StartedAt.Format("2006-01-02 15:04"), state, ss.Summary)
	}
	return nil, queryOutput{Result: sb.String()}, nil
}

func handleMemRescope(_ context.Context, _ *mcp.CallToolRequest, in memRescopeMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Topic == "" || in.Scope == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_rescope: topic and scope are required")
	}
	from := orDefault(in.FromScope, "project")
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_rescope: store close failed", "err", cerr)
		}
	}()
	obs, changed, err := s.Rescope(in.Topic, from, in.Scope, "")
	if err != nil {
		return nil, queryOutput{}, err
	}
	switch {
	case changed:
		return nil, queryOutput{Result: fmt.Sprintf("rescoped %s: %s → %s", obs.TopicKey, from, in.Scope)}, nil
	case obs.TopicKey != "":
		return nil, queryOutput{Result: fmt.Sprintf("%s already in scope %s", in.Topic, in.Scope)}, nil
	default:
		return nil, queryOutput{Result: fmt.Sprintf("no observation found for %s in scope %s", in.Topic, from)}, nil
	}
}

func handleMemDelete(_ context.Context, _ *mcp.CallToolRequest, in memDeleteMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Topic == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_delete: topic is required")
	}
	scope := orDefault(in.Scope, "project")
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("mem_delete: store close failed", "err", cerr)
		}
	}()
	ok, err := s.Delete(in.Topic, scope, "")
	if err != nil {
		return nil, queryOutput{}, err
	}
	if ok {
		return nil, queryOutput{Result: fmt.Sprintf("deleted %s", in.Topic)}, nil
	}
	return nil, queryOutput{Result: fmt.Sprintf("no observation found for %s in scope %s", in.Topic, scope)}, nil
}

// formatObservations renders observations as plain text for MCP output. When
// stale[o.ID] > 0, a warning line is inserted so a cold session knows the note
// references graph symbols that no longer exist.
func formatObservations(obs []memory.Observation, stale map[string]int) string {
	if len(obs) == 0 {
		return "no observations found"
	}
	var sb strings.Builder
	for _, o := range obs {
		key := o.TopicKey
		if key == "" {
			key = o.Title
		}
		fmt.Fprintf(&sb, "[%s] rev:%d %s\n", key, o.Revision, o.UpdatedAt.Format("2006-01-02"))
		if n := stale[o.ID]; n > 0 {
			fmt.Fprintf(&sb, "⚠ possibly stale: %d linked symbol(s) no longer in the graph — verify before trusting\n", n)
		}
		fmt.Fprintf(&sb, "%s\n\n", o.Content)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// refreshGraph opens the store and re-extracts the graph from the working directory.
// Warnings are logged but errors do not prevent the server from starting.
func refreshGraph() {
	s, err := openGraphStore()
	if err != nil {
		slog.Warn("mcp: could not open graph store; queries may return stale data", "err", err)
		return
	}
	defer func() {
		if err := s.Close(); err != nil {
			slog.Warn("mcp: graph store close failed", "err", err)
		}
	}()
	if _, err := extract.Build(repoRoot(), extract.Extensions(), s); err != nil {
		slog.Warn("mcp: graph refresh failed; queries may return stale data", "err", err)
	}
}

// testGapsInput holds the parameters for the test_gaps tool.
type testGapsInput struct {
	Domain    string  `json:"domain,omitempty"     jsonschema:"restrict to files of one domain skill (optional)"`
	Top       int     `json:"top,omitempty"        jsonschema:"maximum symbols to return (default 20)"`
	Coverage  string  `json:"coverage,omitempty"   jsonschema:"path to a coverage report (go/jacoco/coverage.py/istanbul; format auto-detected)"`
	Cover     bool    `json:"cover,omitempty"      jsonschema:"generate coverage by running the suite, then rank by it"`
	FailUnder float64 `json:"fail_under,omitempty" jsonschema:"if >0, append a coverage gate verdict (overall covered% vs threshold); requires coverage or cover"`
}

func handleTestGaps(_ context.Context, _ *mcp.CallToolRequest, in testGapsInput) (*mcp.CallToolResult, queryOutput, error) {
	top := in.Top
	if top <= 0 {
		top = 20
	}
	out, gate, err := runTestGaps(in.Domain, top, 4, 2, false, in.Coverage, in.Cover, in.FailUnder)
	if err != nil {
		return nil, queryOutput{}, err
	}
	if gate != nil {
		out += "\n" + gate.Summary() + "\n"
	}
	return nil, queryOutput{Result: out}, nil
}

// getConceptInput holds the parameters for the get_concept tool.
type getConceptInput struct {
	Name string `json:"name" jsonschema:"concept card name; empty lists all concept names with descriptions"`
}

func handleGetConcept(_ context.Context, _ *mcp.CallToolRequest, in getConceptInput) (*mcp.CallToolResult, queryOutput, error) {
	st, err := openGraphStore()
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("get_concept: %w", err)
	}
	defer st.Close()

	if in.Name != "" {
		row, ok, err := st.GetConcept(in.Name)
		if err != nil {
			return nil, queryOutput{}, fmt.Errorf("get_concept: %w", err)
		}
		if !ok {
			return nil, queryOutput{}, fmt.Errorf("get_concept: no concept %q", in.Name)
		}
		return nil, queryOutput{Result: row.Content}, nil
	}

	rows, err := st.ListConcepts()
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("get_concept: %w", err)
	}
	const conceptListCap = 50
	var sb strings.Builder
	for i, r := range rows {
		if i == conceptListCap {
			fmt.Fprintf(&sb, "…and %d more — pass a name to read one\n", len(rows)-conceptListCap)
			break
		}
		fmt.Fprintf(&sb, "- %s: %s\n", r.Name, r.Description)
	}
	return nil, queryOutput{Result: sb.String()}, nil
}

// setConceptDefinitionInput holds the parameters for the set_concept_definition tool.
type setConceptDefinitionInput struct {
	Name       string `json:"name"       jsonschema:"concept name to update (as listed by get_concept)"`
	Definition string `json:"definition" jsonschema:"one-line, domain-meaning description to store for the concept"`
}

func handleSetConceptDefinition(_ context.Context, _ *mcp.CallToolRequest, in setConceptDefinitionInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Name == "" || in.Definition == "" {
		return nil, queryOutput{}, fmt.Errorf("set_concept_definition: name and definition are required")
	}
	if err := setConceptDefinition(in.Name, in.Definition); err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: fmt.Sprintf("set definition for %q", in.Name)}, nil
}

// getTraitsInput holds the parameters for the get_traits tool.
type getTraitsInput struct {
	Symbol     string `json:"symbol"      jsonschema:"type or interface name (or node ID) to assemble the trait view for"`
	Depth      int    `json:"depth"       jsonschema:"impact BFS depth (default 2)"`
	MaxResults int    `json:"max_results" jsonschema:"maximum impact nodes (default 50)"`
}

func handleGetTraits(_ context.Context, _ *mcp.CallToolRequest, in getTraitsInput) (*mcp.CallToolResult, queryOutput, error) {
	depth := in.Depth
	if depth <= 0 {
		depth = 2
	}
	max := in.MaxResults
	if max <= 0 {
		max = 50
	}
	res, err := runGraphTraits(in.Symbol, depth, max)
	if err != nil {
		return nil, queryOutput{}, err
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("get_traits: marshal: %w", err)
	}
	return nil, queryOutput{Result: string(data)}, nil
}

// testScaffoldInput holds the parameters for the test_scaffold tool.
type testScaffoldInput struct {
	Target string `json:"target" jsonschema:"a function or class symbol (or node ID); a class returns one scaffold per exported method"`
}

func handleTestScaffold(_ context.Context, _ *mcp.CallToolRequest, in testScaffoldInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Target == "" {
		return nil, queryOutput{}, fmt.Errorf("test_scaffold: target is required")
	}
	out, err := runTestScaffold(in.Target)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

// testMutationInput holds the parameters for the test_mutation tool.
type testMutationInput struct {
	Target string `json:"target" jsonschema:"the function or class symbol (or node ID) whose package to mutation-test"`
}

func handleTestMutation(ctx context.Context, _ *mcp.CallToolRequest, in testMutationInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Target == "" {
		return nil, queryOutput{}, fmt.Errorf("test_mutation: target is required")
	}
	out, err := runTestMutation(ctx, in.Target)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

// flowInput holds the parameters for the get_flow tool. Note: an absent JSON
// field decodes to 0, so 0 cannot mean "unlimited" here — the handler treats
// any value <= 0 as the default. Unlimited fan-out is CLI-only (--fan-out 0).
type flowInput struct {
	Symbol string `json:"symbol"  jsonschema:"entry point symbol name (or node ID) to trace"`
	Depth  int    `json:"depth"   jsonschema:"call tree depth (default 5)"`
	FanOut int    `json:"fan_out" jsonschema:"maximum direct callees per node (default 10)"`
}

func handleGetFlow(_ context.Context, _ *mcp.CallToolRequest, in flowInput) (*mcp.CallToolResult, queryOutput, error) {
	depth := in.Depth
	if depth <= 0 {
		depth = 5
	}
	fanOut := in.FanOut
	if fanOut <= 0 {
		fanOut = 10
	}
	res, err := runGraphFlow(in.Symbol, depth, fanOut)
	if err != nil {
		return nil, queryOutput{}, err
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("get_flow: marshal: %w", err)
	}
	return nil, queryOutput{Result: string(data)}, nil
}

// cyclesInput holds the parameters for the get_cycles tool (none today).
type cyclesInput struct{}

func handleCycles(_ context.Context, _ *mcp.CallToolRequest, _ cyclesInput) (*mcp.CallToolResult, queryOutput, error) {
	out, err := runGraphCycles(false)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

// bridgesInput holds the parameters for the get_bridges tool.
type bridgesInput struct {
	Top int `json:"top" jsonschema:"number of chokepoints to return (default 20)"`
}

func handleBridges(_ context.Context, _ *mcp.CallToolRequest, in bridgesInput) (*mcp.CallToolResult, queryOutput, error) {
	top := in.Top
	if top <= 0 {
		top = 20
	}
	out, err := runGraphBridges(top, 100, false)
	if err != nil {
		return nil, queryOutput{}, err
	}
	return nil, queryOutput{Result: out}, nil
}

// mcpToolNames lists every tool registered by newMCPServer.
// Keep in sync when adding tools — TestMCPListFlag guards this set.
var mcpToolNames = []string{
	"get_impact", "get_context", "find_symbol",
	"mem_save", "mem_search", "mem_recent",
	"mem_rescope", "mem_delete",
	"mem_export", "mem_import",
	"test_gaps", "test_scaffold", "test_mutation",
	"get_concept", "set_concept_definition", "get_traits", "get_flow", "get_bridges", "get_cycles",
	"mem_relate", "mem_related", "mem_clusters", "mem_conflicts", "crystallize_save",
	"mem_session_start", "mem_session_end", "mem_session_list",
}

// printMCPTools writes one tool name per line to w.
func printMCPTools(w io.Writer) {
	for _, name := range mcpToolNames {
		fmt.Fprintln(w, name)
	}
}

// newMCPServer creates and configures the MCP server with all graph and memory tools.
func newMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "tu-agent",
		Version: version,
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_impact",
		Description: "Return the blast-radius of a change: which symbols and files are affected if target is modified.",
	}, handleImpact)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_context",
		Description: "Return all context relevant to touching a target: blast radius, associated skills, conventions, and tests.",
	}, handleContext)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_symbol",
		Description: "Locate where a symbol is defined in the codebase.",
	}, handleFind)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_save",
		Description: "Save (upsert) an observation to project memory by topic key. Re-saving the same topic refines it and bumps its revision. Use scope 'personal' to keep a note local (not shared in the team chunk).",
	}, handleMemSave)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_rescope",
		Description: "Change an existing observation's scope in place (e.g. project→personal to pull a note out of the shared chunk). Recomputes its sync_id; preserves content and revision.",
	}, handleMemRescope)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_delete",
		Description: "Soft-delete an observation by topic (and optional scope). It drops out of search and the next chunk export. Local only — does not delete teammates' copies.",
	}, handleMemDelete)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_search",
		Description: "Search project memory for observations matching a keyword query.",
	}, handleMemSearch)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_recent",
		Description: "Return the most recent N observations from project memory (default 5).",
	}, handleMemRecent)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_clusters",
		Description: "List dense clusters of related memory notes worth consolidating into a project skill. Deterministic — no LLM, no writes. Optional `min` (default 5) sets the cluster-size threshold.",
	}, handleMemClusters)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_conflicts",
		Description: "List recorded contradictions between notes (conflicts_with edges), each resolved to both notes' topic keys. Record new ones with mem_relate type conflicts_with; resolution (delete/rescope/supersede) is the human's.",
	}, handleMemConflicts)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "crystallize_save",
		Description: "Store a generated skill body as the canonical skill/<label> record and materialize it locally. The binary adds provenance; pass the SKILL.md body without a provenance marker. Pair with mem_clusters (pick a cluster) and Claude Code generation.",
	}, handleCrystallizeSave)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_export",
		Description: "Write your authored observations to a committed chunk file for the team to share via git.",
	}, handleMemExport)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_import",
		Description: "Merge teammates' committed memory chunk files into the local store.",
	}, handleMemImport)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "test_gaps",
		Description: "Rank untested public functions by risk (fan-in × blast radius from the knowledge graph). Deterministic — no LLM, no build step.",
	}, handleTestGaps)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "test_scaffold",
		Description: "Return the deterministic half of test generation for a function or class: prompt context (signature, body, call sites, callees, domain notes), test file path, scoped run command, and language conventions. Returns a JSON array of scaffolds (one per method for a class). No LLM call, no writes.",
	}, handleTestScaffold)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "test_mutation",
		Description: "Run mutation testing on a symbol's package (opt-in; requires an external engine such as go-mutesting, PIT, mutmut, or Stryker). Degrades to a skipped report when the tool is absent.",
	}, handleTestMutation)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_concept",
		Description: "Return the concept card (SKILL.md) for a named concept, or list all concepts with descriptions when name is empty.",
	}, handleGetConcept)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_concept_definition",
		Description: "Set a concept's one-line definition in the graph store (deterministic, no model call). Use to record a generative concept definition; pair with get_concept to read the card first.",
	}, handleSetConceptDefinition)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_traits",
		Description: "Trait-centric view of a symbol: which interfaces a type implements and where the logic lives (as_type), plus who implements an interface and what the blast radius is (as_interface). JSON output.",
	}, handleGetTraits)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_flow",
		Description: "Trace the execution call tree from an entry symbol: depth-limited walk over calls edges with package-boundary annotations and interface dispatch candidates. JSON, identical to 'graph flow --json'.",
	}, handleGetFlow)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_bridges",
		Description: "List architectural chokepoints (high-betweenness bridge nodes) in the call graph.",
	}, handleBridges)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_cycles",
		Description: "List dependency cycles (strongly-connected components). Use to find tangled, co-coupled clusters to refactor.",
	}, handleCycles)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_relate",
		Description: "Link an observation to a graph node (or another observation) with a typed relation.",
	}, handleMemRelate)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_related",
		Description: "List observations linked to a graph node.",
	}, handleMemRelated)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_session_start",
		Description: "Start a work session; returns the previous session's summary.",
	}, handleMemSessionStart)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_session_end",
		Description: "End the active work session; composes a summary from observations if none given.",
	}, handleMemSessionEnd)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mem_session_list",
		Description: "List recent work sessions.",
	}, handleMemSessionList)

	return srv
}

var mcpListTools bool

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a stdio MCP server exposing graph query tools to Claude Code",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpListTools {
			printMCPTools(cmd.OutOrStdout())
			return nil
		}
		refreshGraph()
		srv := newMCPServer()
		return srv.Run(cmd.Context(), &mcp.StdioTransport{})
	},
}

func init() {
	mcpCmd.Flags().BoolVar(&mcpListTools, "list", false, "list available MCP tools and exit")
	rootCmd.AddCommand(mcpCmd)
}
