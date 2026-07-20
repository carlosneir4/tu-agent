package telemetry

import "time"

// Entry is one row in the telemetry JSONL log.
// Every model API call appends one Entry.
type Entry struct {
	Timestamp      time.Time `json:"timestamp"`
	Provider       string    `json:"provider,omitempty"`
	Model          string    `json:"model,omitempty"`
	InputTokens    int       `json:"input_tokens,omitempty"`
	OutputTokens   int       `json:"output_tokens,omitempty"`
	LatencyMS      int64     `json:"latency_ms,omitempty"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	SubAgent       string    `json:"sub_agent,omitempty"`
	ToolCallsCount int       `json:"tool_calls_count,omitempty"`
	// Event marks non-model rows (e.g. "load_skill"). Empty for model calls.
	Event string `json:"event,omitempty"`
	// Skill is the skill name for load_skill events.
	Skill string `json:"skill,omitempty"`
	// Found reports whether a load_skill event hit an indexed skill.
	Found bool `json:"found"`
	// Tool is the name of the tool invoked, for mcp_call events.
	Tool string `json:"tool,omitempty"`
	// DurationMS is the wall-clock duration of the call in milliseconds.
	DurationMS int64 `json:"duration_ms,omitempty"`
	// OK reports whether the call succeeded. With omitempty, ok:true is written
	// and ok:false is omitted, so absent-means-false on read. Only meaningful for
	// call-style events (Event=="mcp_call"); other events (load_skill,
	// graph_refresh, ...) leave it unset.
	OK bool `json:"ok,omitempty"`
	// Error carries a failure message for call/hook-style events; empty when the
	// call succeeded.
	Error string `json:"error,omitempty"`
	// ResultBytes is the size in bytes of the serialized result.
	ResultBytes int `json:"result_bytes,omitempty"`
	// ZeroResult reports whether a query-style call returned no results.
	ZeroResult bool `json:"zero_result,omitempty"`
	// Stage is the tdd stage name, for tdd_stage events.
	Stage string `json:"stage,omitempty"`
	// Outcome is the result of a stage or gate, for tdd_stage/violation events.
	Outcome string `json:"outcome,omitempty"`
	// SessionID identifies the work session this entry belongs to.
	SessionID string `json:"session_id,omitempty"`
	// Parsed/Unchanged/Deleted/Failed carry graph_refresh (staleness) counts.
	Parsed    int `json:"parsed,omitempty"`
	Unchanged int `json:"unchanged,omitempty"`
	Deleted   int `json:"deleted,omitempty"`
	Failed    int `json:"failed,omitempty"`
	// Feature is the tdd feature slug, for gate_attempt/tdd_stage events.
	Feature string `json:"feature,omitempty"`
}

// Event values identify non-model telemetry rows.
const (
	EventLoadSkill    = "load_skill"
	EventMCPCall      = "mcp_call"
	EventHook         = "hook"
	EventGraphRefresh = "graph_refresh"
	EventTddStage     = "tdd_stage"
	EventViolation    = "violation"
	EventMutation     = "mutation"
	EventPrompt       = "prompt"
	EventGateAttempt  = "gate_attempt"
	EventSkillInvoked = "skill_invoked"
)
