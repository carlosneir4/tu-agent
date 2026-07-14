package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/advise"
	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/reconcile"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// mcpTelemetryLogger, when non-nil, receives one Entry per MCP tool call.
// It defaults to nil so unit tests that build a server via newMCPServer()
// never write telemetry; only mcpCmd's RunE wires a real logger.
var mcpTelemetryLogger *telemetry.Logger

// selfReportingMCPTools lists tools whose handlers emit their own, richer
// telemetry row (e.g. with ZeroResult), so mcpTelemetryMiddleware must not
// double-emit a generic row for them.
var selfReportingMCPTools = map[string]bool{
	"mem_search": true,
}

// mcpToolName extracts the tool name from a tools/call request, or "" if the
// request is not a tool call (or the concrete params type is unexpected).
func mcpToolName(req mcp.Request) string {
	if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
		return p.Name
	}
	return ""
}

// resultByteLen best-effort measures the serialized size of an MCP result,
// preferring the tool's Content payload when res is a CallToolResult. It
// tolerates a typed-nil *mcp.CallToolResult (which the SDK returns alongside a
// jsonrpc error for unknown tools or handler errors): the interface is non-nil
// but the pointer inside is nil, so ctr != nil is checked before dereferencing.
func resultByteLen(res mcp.Result) int {
	if ctr, ok := res.(*mcp.CallToolResult); ok && ctr != nil {
		data, err := json.Marshal(ctr.Content)
		if err != nil {
			return 0
		}
		return len(data)
	}
	data, err := json.Marshal(res)
	if err != nil {
		return 0
	}
	return len(data)
}

// recordMCPCall best-effort logs one telemetry row for an MCP tool call.
// It is a no-op when mcpTelemetryLogger is nil.
func recordMCPCall(name string, dur time.Duration, res mcp.Result, err error) {
	if mcpTelemetryLogger == nil {
		return
	}
	ok := err == nil
	// A typed-nil *mcp.CallToolResult accompanies a jsonrpc error (unknown tool,
	// handler error); the type assertion succeeds with ctr==nil, so guard the
	// dereference. err != nil already forces ok=false in that case.
	if ctr, isCTR := res.(*mcp.CallToolResult); isCTR && ctr != nil && ctr.IsError {
		ok = false
	}
	_ = mcpTelemetryLogger.Log(telemetry.Entry{
		Timestamp:   time.Now(),
		Event:       telemetry.EventMCPCall,
		Tool:        name,
		DurationMS:  dur.Milliseconds(),
		OK:          ok,
		ResultBytes: resultByteLen(res),
	})
}

// mcpTelemetryMiddleware records one telemetry row per MCP tool call, unless
// the tool is self-reporting (see selfReportingMCPTools) or mcpTelemetryLogger
// is nil.
func mcpTelemetryMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		name := mcpToolName(req)
		if selfReportingMCPTools[name] {
			return next(ctx, method, req)
		}
		start := time.Now()
		res, err := next(ctx, method, req)
		recordMCPCall(name, time.Since(start), res, err)
		return res, err
	}
}

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

// memReconcileMCPInput holds the parameters for the mem_reconcile tool.
type memReconcileMCPInput struct {
	Min          int    `json:"min,omitempty"        jsonschema:"minimum notes for a cluster to be considered live (default 5)"`
	Apply        bool   `json:"apply,omitempty"      jsonschema:"apply the reconcile (rebind/rename); absent = dry-run"`
	Topic        string `json:"topic,omitempty"      jsonschema:"selector: scope the apply to ONE orphan by topic key (e.g. skill/acme-orphan)"`
	ToCluster    string `json:"to_cluster,omitempty" jsonschema:"force the re-point target cluster label (requires topic)"`
	Name         string `json:"name,omitempty"       jsonschema:"rename skill/<old> -> skill/<new> (record + folder; requires topic)"`
	PruneFolders bool   `json:"prune_folders,omitempty" jsonschema:"actually delete orphaned crystallize-marked skill folders (only meaningful with apply=true); absent/false = dry-run, candidates are reported as would-remove but left on disk"`
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
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, err := s.Upsert(in.Topic, in.Content, memory.UpsertOpts{Scope: in.Scope, Type: in.Type, Source: "mcp", Author: gitAuthor()})
		if err != nil {
			return err
		}
		out = queryOutput{Result: fmt.Sprintf("saved %s rev:%d", obs.TopicKey, obs.Revision)}
		return nil
	})
	return nil, out, err
}

// recordMemSearch best-effort emits mem_search's own telemetry row. mem_search
// is self-reporting (the middleware skips it), so both its success and failure
// paths must log here or a failure would leave no row at all. No-op when the
// logger is nil.
func recordMemSearch(dur time.Duration, ok bool, resultBytes int, zeroResult bool) {
	if mcpTelemetryLogger == nil {
		return
	}
	_ = mcpTelemetryLogger.Log(telemetry.Entry{
		Timestamp:   time.Now(),
		Event:       telemetry.EventMCPCall,
		Tool:        "mem_search",
		DurationMS:  dur.Milliseconds(),
		OK:          ok,
		ResultBytes: resultBytes,
		ZeroResult:  zeroResult,
	})
}

func handleMemSearch(_ context.Context, _ *mcp.CallToolRequest, in memSearchMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	start := time.Now()
	if in.Query == "" {
		recordMemSearch(time.Since(start), false, 0, false)
		return nil, queryOutput{}, fmt.Errorf("mem_search: query is required")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, total, err := s.Search(in.Query, in.Type, limit)
		if err != nil {
			return err
		}
		result := formatObservations(obs, recallStale(s, obs))
		if total > len(obs) {
			result += fmt.Sprintf("\n\nshowing %d of %d — refine the query or raise limit", len(obs), total)
		}
		recordMemSearch(time.Since(start), true, len(result), total == 0)
		out = queryOutput{Result: result}
		return nil
	})
	if err != nil {
		recordMemSearch(time.Since(start), false, 0, false)
		return nil, queryOutput{}, err
	}
	return nil, out, nil
}

func handleMemRecent(_ context.Context, _ *mcp.CallToolRequest, in memRecentMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	n := in.N
	if n <= 0 {
		n = 5
	}
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, err := s.Recent(n)
		if err != nil {
			return err
		}
		out = queryOutput{Result: formatObservations(obs, recallStale(s, obs))}
		return nil
	})
	return nil, out, err
}

func handleMemClusters(_ context.Context, _ *mcp.CallToolRequest, in memClustersMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	min := in.Min
	if min <= 0 {
		min = 5
	}
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, err := s.List()
		if err != nil {
			return err
		}
		out = queryOutput{Result: crystallize.Format(crystallize.Detect(obs, min))}
		return nil
	})
	return nil, out, err
}

// adviseMCPInput is the (empty) parameter set for the advise tool.
type adviseMCPInput struct{}

// handleAdvise is the read-only, on-demand diagnostic counterpart to `tu-agent
// advise` (plain mode): same Inputs (telemetry insights + crystallize
// needs), same Evaluate rules, dismissed rules dropped. Unlike advise
// --nudge (the deduped SessionStart hook channel), it never persists dedup
// state — every call re-evaluates from scratch.
func handleAdvise(_ context.Context, _ *mcp.CallToolRequest, _ adviseMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	root := repoRoot()
	in, err := adviseInputs(root)
	if err != nil {
		return nil, queryOutput{}, err
	}
	st, err := loadAdviseState(root)
	if err != nil {
		return nil, queryOutput{}, err
	}
	var sb strings.Builder
	for _, s := range advise.Evaluate(in) {
		if st.Rules[s.RuleID].Dismissed {
			continue
		}
		sb.WriteString(s.Message)
		sb.WriteString("\n")
	}
	return nil, queryOutput{Result: sb.String()}, nil
}

func handleMemConflicts(_ context.Context, _ *mcp.CallToolRequest, _ memConflictsMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		rels, err := s.RelationsByType("conflicts_with")
		if err != nil {
			return err
		}
		byID, err := conflictTopicMap(s, rels)
		if err != nil {
			return err
		}
		out = queryOutput{Result: formatConflicts(rels, byID)}
		return nil
	})
	return nil, out, err
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
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		author := gitAuthor()
		recs, err := s.ExportRecords(author)
		if err != nil {
			return err
		}
		recs, excluded := filterSecretRecords(recs)
		if len(recs) > 0 {
			author, err = requireAuthor(author)
			if err != nil {
				return err
			}
		}
		path, written, err := memory.WriteChunk(memoryChunksDir(repoRoot()), author, recs)
		if err != nil {
			return err
		}
		status := "unchanged"
		if written {
			status = "wrote"
		}
		result := fmt.Sprintf("%s %s (%d observations)", status, path, len(recs))
		if len(excluded) > 0 {
			result += fmt.Sprintf(" (excluded %d with apparent secrets)", len(excluded))
		}
		out = queryOutput{Result: result}
		return nil
	})
	return nil, out, err
}

func handleMemImport(_ context.Context, _ *mcp.CallToolRequest, _ memImportMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	recs, err := memory.ReadAllChunks(memoryChunksDir(repoRoot()))
	if err != nil {
		return nil, queryOutput{}, err
	}
	var out queryOutput
	err = withMemStore(repoRoot(), func(s *memory.Store) error {
		res, err := s.ImportRecords(recs)
		if err != nil {
			return err
		}
		out = queryOutput{Result: fmt.Sprintf("imported: %d new, %d updated, %d unchanged",
			res.Inserted, res.Updated, res.Skipped)}
		return nil
	})
	return nil, out, err
}

func handleMemRelate(_ context.Context, _ *mcp.CallToolRequest, in memRelateInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.From == "" || in.To == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_relate: from and to are required")
	}
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		rel, err := s.Relate(in.From, in.To, in.Type)
		if err != nil {
			return err
		}
		out = queryOutput{Result: fmt.Sprintf("linked %s --%s--> %s", rel.FromID, rel.Type, rel.ToID)}
		return nil
	})
	return nil, out, err
}

func handleMemRelated(_ context.Context, _ *mcp.CallToolRequest, in memRelatedInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.NodeID == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_related: node_id is required")
	}
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		rels, err := s.RelationsTo([]string{in.NodeID})
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(rels))
		for _, r := range rels {
			ids = append(ids, r.FromID)
		}
		obs, err := s.ObservationsByID(ids)
		if err != nil {
			return err
		}
		out = queryOutput{Result: formatObservations(obs, recallStale(s, obs))}
		return nil
	})
	return nil, out, err
}

func handleMemSessionStart(_ context.Context, _ *mcp.CallToolRequest, in memSessionStartInput) (*mcp.CallToolResult, queryOutput, error) {
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		sess, prev, err := s.SessionStart(in.Project)
		if err != nil {
			return err
		}
		res := fmt.Sprintf("session started: %s", sess.ID)
		if prev != "" {
			res += "\n\n## Last session\n" + prev
		}
		out = queryOutput{Result: res}
		return nil
	})
	return nil, out, err
}

func handleMemSessionEnd(_ context.Context, _ *mcp.CallToolRequest, in memSessionEndInput) (*mcp.CallToolResult, queryOutput, error) {
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		sess, err := s.SessionEnd(in.Project, in.Summary)
		if err != nil {
			return err
		}
		out = queryOutput{Result: "session ended: " + sess.ID + "\nsummary: " + sess.Summary}
		return nil
	})
	return nil, out, err
}

func handleMemSessionList(_ context.Context, _ *mcp.CallToolRequest, in memSessionListInput) (*mcp.CallToolResult, queryOutput, error) {
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		list, err := s.SessionList("", in.N)
		if err != nil {
			return err
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
		out = queryOutput{Result: sb.String()}
		return nil
	})
	return nil, out, err
}

func handleMemRescope(_ context.Context, _ *mcp.CallToolRequest, in memRescopeMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Topic == "" || in.Scope == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_rescope: topic and scope are required")
	}
	from := orDefault(in.FromScope, "project")
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, changed, err := s.Rescope(in.Topic, from, in.Scope, "")
		if err != nil {
			return err
		}
		switch {
		case changed:
			out = queryOutput{Result: fmt.Sprintf("rescoped %s: %s → %s", obs.TopicKey, from, in.Scope)}
		case obs.TopicKey != "":
			out = queryOutput{Result: fmt.Sprintf("%s already in scope %s", in.Topic, in.Scope)}
		default:
			out = queryOutput{Result: fmt.Sprintf("no observation found for %s in scope %s", in.Topic, from)}
		}
		return nil
	})
	return nil, out, err
}

func handleMemDelete(_ context.Context, _ *mcp.CallToolRequest, in memDeleteMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if in.Topic == "" {
		return nil, queryOutput{}, fmt.Errorf("mem_delete: topic is required")
	}
	scope := orDefault(in.Scope, "project")
	var out queryOutput
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		ok, err := s.Delete(in.Topic, scope, "")
		if err != nil {
			return err
		}
		if ok {
			out = queryOutput{Result: fmt.Sprintf("deleted %s", in.Topic)}
		} else {
			out = queryOutput{Result: fmt.Sprintf("no observation found for %s in scope %s", in.Topic, scope)}
		}
		return nil
	})
	return nil, out, err
}

// handleMemReconcile reconciles orphaned skill records against the current
// corpus. It defaults to dry-run (mutating nothing) via the SAME
// renderReconcilePlan adapter the CLI uses; with apply=true it routes through
// the SAME applyReconcile adapter the CLI `--apply` path uses, so both surfaces
// emit byte-identical text (§10 parity).
func handleMemReconcile(_ context.Context, _ *mcp.CallToolRequest, in memReconcileMCPInput) (*mcp.CallToolResult, queryOutput, error) {
	if err := validateReconcileTargeting(in.Apply, in.Topic, in.ToCluster, in.Name); err != nil {
		return nil, queryOutput{}, err
	}
	minSize := in.Min
	if minSize <= 0 {
		minSize = 5
	}
	root := repoRoot()
	var out queryOutput
	err := withMemStore(root, func(s *memory.Store) error {
		skillsDir := generatedSkillsDir(root)
		if !in.Apply {
			text, err := renderReconcilePlan(s, skillsDir, minSize)
			if err != nil {
				return err
			}
			out = queryOutput{Result: text}
			return nil
		}
		text, err := applyReconcile(s, skillsDir, minSize, in.Topic,
			reconcile.ApplyOptions{Name: in.Name, ToCluster: in.ToCluster, PruneFolders: in.PruneFolders})
		if err != nil {
			return err
		}
		out = queryOutput{Result: text}
		return nil
	})
	return nil, out, err
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

// getArchitectureInput holds the (empty) parameters for the get_architecture tool.
type getArchitectureInput struct{}

func handleGetArchitecture(_ context.Context, _ *mcp.CallToolRequest, _ getArchitectureInput) (*mcp.CallToolResult, queryOutput, error) {
	body, err := loadArchitecture()
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("get_architecture: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return nil, queryOutput{Result: "No architecture overview yet — run /tu-agent:synthesize first."}, nil
	}
	return nil, queryOutput{Result: body}, nil
}

// setArchitectureInput holds the parameters for the set_architecture tool.
type setArchitectureInput struct {
	Content string `json:"content" jsonschema:"the architecture overview markdown to persist (frontmatter, if any, is stripped)"`
}

func handleSetArchitecture(_ context.Context, _ *mcp.CallToolRequest, in setArchitectureInput) (*mcp.CallToolResult, queryOutput, error) {
	wrote, err := persistArchitecture(in.Content)
	if err != nil {
		return nil, queryOutput{}, fmt.Errorf("set_architecture: %w", err)
	}
	if !wrote {
		return nil, queryOutput{}, fmt.Errorf("set_architecture: empty content after stripping frontmatter")
	}
	return nil, queryOutput{Result: "stored architecture overview"}, nil
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

// printMCPTools writes one tool name per line to w, derived from the real
// registry so the --list output can never drift from the tools actually served.
func printMCPTools(w io.Writer) {
	_, names := buildMCPServer()
	for _, name := range names {
		fmt.Fprintln(w, name)
	}
}

// addTool registers a tool on the server and records its name in names, so the
// list of served tools is derived from the same call sites that register them.
func addTool[In, Out any](srv *mcp.Server, names *[]string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	*names = append(*names, t.Name)
	mcp.AddTool(srv, t, h)
}

// newMCPServer creates and configures the MCP server with all graph and memory tools.
func newMCPServer() *mcp.Server {
	srv, _ := buildMCPServer()
	return srv
}

// buildMCPServer builds the MCP server and returns it together with the names of
// every tool actually registered on it, in registration order.
func buildMCPServer() (*mcp.Server, []string) {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "tu-agent",
		Version: version,
	}, nil)

	srv.AddReceivingMiddleware(mcpTelemetryMiddleware)

	var names []string

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_impact",
		Description: "Return the blast-radius of a change: which symbols and files are affected if target is modified.",
	}, handleImpact)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_context",
		Description: "Return all context relevant to touching a target: blast radius, associated skills, conventions, and tests.",
	}, handleContext)

	addTool(srv, &names, &mcp.Tool{
		Name:        "find_symbol",
		Description: "Locate where a symbol is defined in the codebase.",
	}, handleFind)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_save",
		Description: "Save (upsert) an observation to project memory by topic key. Re-saving the same topic refines it and bumps its revision. Use scope 'personal' to keep a note local (not shared in the team chunk).",
	}, handleMemSave)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_rescope",
		Description: "Change an existing observation's scope in place (e.g. project→personal to pull a note out of the shared chunk). Recomputes its sync_id; preserves content and revision.",
	}, handleMemRescope)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_delete",
		Description: "Soft-delete an observation by topic (and optional scope). It drops out of search and the next chunk export. Local only — does not delete teammates' copies.",
	}, handleMemDelete)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_reconcile",
		Description: "Reconcile orphaned skill records — crystallized skills whose bound cluster label no longer matches any live cluster — against the current corpus. Defaults to a read-only dry-run plan; pass apply=true (optionally topic + to_cluster/name to scope one orphan) to rebind or rename. Orphaned crystallize-marked skill folders are reported but never deleted unless prune_folders=true is also passed.",
	}, handleMemReconcile)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_search",
		Description: "Search project memory for observations matching a keyword query.",
	}, handleMemSearch)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_recent",
		Description: "Return the most recent N observations from project memory (default 5).",
	}, handleMemRecent)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_clusters",
		Description: "List dense clusters of related memory notes worth consolidating into a project skill. Deterministic — no LLM, no writes. Optional `min` (default 5) sets the cluster-size threshold.",
	}, handleMemClusters)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_conflicts",
		Description: "List recorded contradictions between notes (conflicts_with edges), each resolved to both notes' topic keys. Record new ones with mem_relate type conflicts_with; resolution (delete/rescope/supersede) is the human's.",
	}, handleMemConflicts)

	addTool(srv, &names, &mcp.Tool{
		Name:        "crystallize_save",
		Description: "Store a generated skill body as the canonical skill/<label> record and materialize it locally. The binary adds provenance; pass the SKILL.md body without a provenance marker. Pair with mem_clusters (pick a cluster) and Claude Code generation.",
	}, handleCrystallizeSave)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_export",
		Description: "Write your authored observations to a committed chunk file for the team to share via git.",
	}, handleMemExport)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_import",
		Description: "Merge teammates' committed memory chunk files into the local store.",
	}, handleMemImport)

	addTool(srv, &names, &mcp.Tool{
		Name:        "test_gaps",
		Description: "Rank untested public functions by risk (fan-in × blast radius from the knowledge graph). Deterministic — no LLM, no build step.",
	}, handleTestGaps)

	addTool(srv, &names, &mcp.Tool{
		Name:        "test_scaffold",
		Description: "Return the deterministic half of test generation for a function or class: prompt context (signature, body, call sites, callees, domain notes), test file path, scoped run command, and language conventions. Returns a JSON array of scaffolds (one per method for a class). No LLM call, no writes.",
	}, handleTestScaffold)

	addTool(srv, &names, &mcp.Tool{
		Name:        "test_mutation",
		Description: "Run mutation testing on a symbol's package (opt-in; requires an external engine such as go-mutesting, PIT, mutmut, or Stryker). Degrades to a skipped report when the tool is absent.",
	}, handleTestMutation)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_concept",
		Description: "Return the concept card (SKILL.md) for a named concept, or list all concepts with descriptions when name is empty.",
	}, handleGetConcept)

	addTool(srv, &names, &mcp.Tool{
		Name:        "set_concept_definition",
		Description: "Set a concept's one-line definition in the graph store (deterministic, no model call). Use to record a generative concept definition; pair with get_concept to read the card first.",
	}, handleSetConceptDefinition)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_architecture",
		Description: "Return the synthesized architecture overview (purpose, domains, how they connect, blast radius) from the graph store. Empty until /tu-agent:synthesize has run.",
	}, handleGetArchitecture)

	addTool(srv, &names, &mcp.Tool{
		Name:        "set_architecture",
		Description: "Persist the architecture overview markdown to the graph store (deterministic write; frontmatter is stripped). Used by the architecture-synthesizer skill to store the narrative it generated.",
	}, handleSetArchitecture)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_traits",
		Description: "Trait-centric view of a symbol: which interfaces a type implements and where the logic lives (as_type), plus who implements an interface and what the blast radius is (as_interface). JSON output.",
	}, handleGetTraits)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_flow",
		Description: "Trace the execution call tree from an entry symbol: depth-limited walk over calls edges with package-boundary annotations and interface dispatch candidates. JSON, identical to 'graph flow --json'.",
	}, handleGetFlow)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_bridges",
		Description: "List architectural chokepoints (high-betweenness bridge nodes) in the call graph.",
	}, handleBridges)

	addTool(srv, &names, &mcp.Tool{
		Name:        "get_cycles",
		Description: "List dependency cycles (strongly-connected components). Use to find tangled, co-coupled clusters to refactor.",
	}, handleCycles)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_relate",
		Description: "Link an observation to a graph node (or another observation) with a typed relation.",
	}, handleMemRelate)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_related",
		Description: "List observations linked to a graph node.",
	}, handleMemRelated)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_session_start",
		Description: "Start a work session; returns the previous session's summary.",
	}, handleMemSessionStart)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_session_end",
		Description: "End the active work session; composes a summary from observations if none given.",
	}, handleMemSessionEnd)

	addTool(srv, &names, &mcp.Tool{
		Name:        "mem_session_list",
		Description: "List recent work sessions.",
	}, handleMemSessionList)

	addTool(srv, &names, &mcp.Tool{
		Name:        "advise",
		Description: "Deterministic, evidence-based suggestions from telemetry insights and crystallize state (e.g. clusters ready to crystallize, repeated secret-guard blocks). Read-only diagnostic — does not persist dedup state; pairs with the SessionStart `advise --nudge` hook, which does.",
	}, handleAdvise)

	return srv, names
}

var mcpListTools bool

// maybeInitMCPTelemetry points mcpTelemetryLogger at the repo telemetry log,
// but only at the "full" telemetry level — at "minimal" it is left at its
// nil default so mcp_call rows are never recorded.
func maybeInitMCPTelemetry() {
	if telemetryLevel() != "full" {
		return
	}
	if lg, err := telemetry.NewLogger(filepath.Join(repoRoot(), ".tu-agent", "telemetry.jsonl")); err == nil {
		mcpTelemetryLogger = lg
	}
}

var mcpCmd = &cobra.Command{
	GroupID: "diagnostics",
	Use:     "mcp",
	Short:   "Run a stdio MCP server exposing graph query tools to Claude Code",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpListTools {
			printMCPTools(cmd.OutOrStdout())
			return nil
		}
		maybeInitMCPTelemetry()
		refreshGraph()
		srv := newMCPServer()
		return srv.Run(cmd.Context(), &mcp.StdioTransport{})
	},
}

func init() {
	mcpCmd.Flags().BoolVar(&mcpListTools, "list", false, "list available MCP tools and exit")
	rootCmd.AddCommand(mcpCmd)
}
