package telemetry

import "time"

// Entry is one row in the telemetry JSONL log.
// Every model API call appends one Entry.
type Entry struct {
	Timestamp      time.Time `json:"timestamp"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	InputTokens    int       `json:"input_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	LatencyMS      int64     `json:"latency_ms"`
	CostUSD        float64   `json:"cost_usd"`
	SubAgent       string    `json:"sub_agent,omitempty"`
	ToolCallsCount int       `json:"tool_calls_count"`
	// Event marks non-model rows (e.g. "load_skill"). Empty for model calls.
	Event string `json:"event,omitempty"`
	// Skill is the skill name for load_skill events.
	Skill string `json:"skill,omitempty"`
	// Found reports whether a load_skill event hit an indexed skill.
	Found bool `json:"found"`
}
