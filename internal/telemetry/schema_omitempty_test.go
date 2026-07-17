package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestEntry_NonModelEventOmitsFrozenHarnessKeys pins @s1: a hook/mcp_call event
// row must not carry the model-only keys with zero values. Before the schema
// gains omitempty on Provider/Model/InputTokens/OutputTokens/LatencyMS/CostUSD/
// ToolCallsCount, these keys serialize with zero values on every event line;
// after omitempty they vanish.
func TestEntry_NonModelEventOmitsFrozenHarnessKeys(t *testing.T) {
	e := Entry{
		Timestamp:  time.Unix(0, 0).UTC(),
		Event:      EventHook,
		Tool:       "graph update",
		DurationMS: 5,
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)

	for _, key := range []string{
		`"provider"`, `"model"`, `"input_tokens"`, `"output_tokens"`,
		`"cost_usd"`, `"latency_ms"`, `"tool_calls_count"`,
	} {
		if strings.Contains(s, key) {
			t.Errorf("non-model event row must omit %s, got: %s", key, s)
		}
	}
}
