package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEntry_NewFieldsRoundtrip(t *testing.T) {
	ts := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	in := Entry{
		Timestamp:   ts,
		Event:       EventMCPCall,
		Tool:        "get_context",
		DurationMS:  42,
		OK:          true,
		ResultBytes: 128,
		ZeroResult:  true,
		Stage:       "red",
		Outcome:     "pass",
		SessionID:   "sess-1",
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out Entry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out != in {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", out, in)
	}
}

func TestEntry_ZeroValueOmitsNewFields(t *testing.T) {
	data, err := json.Marshal(Entry{Timestamp: time.Unix(0, 0)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)

	for _, key := range []string{
		`"tool"`, `"duration_ms"`, `"ok"`, `"result_bytes"`,
		`"zero_result"`, `"stage"`, `"outcome"`, `"session_id"`,
	} {
		if strings.Contains(s, key) {
			t.Errorf("zero-value Entry JSON must omit %s, got: %s", key, s)
		}
	}
}

func TestEntry_OKOmitemptyRoundtrip(t *testing.T) {
	tests := []struct {
		name   string
		ok     bool
		wantIn bool
	}{
		{name: "true present", ok: true, wantIn: true},
		{name: "false absent", ok: false, wantIn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(Entry{Timestamp: time.Unix(0, 0), OK: tt.ok})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := strings.Contains(string(data), `"ok"`)
			if got != tt.wantIn {
				t.Errorf("OK=%v: ok key present=%v, want %v (json: %s)", tt.ok, got, tt.wantIn, data)
			}

			var out Entry
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if out.OK != tt.ok {
				t.Errorf("OK roundtrip: got %v, want %v", out.OK, tt.ok)
			}
		})
	}
}

func TestEntry_GraphCountFieldsRoundtrip(t *testing.T) {
	ts := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	in := Entry{
		Timestamp: ts,
		Event:     EventGraphRefresh,
		Parsed:    10,
		Unchanged: 5,
		Deleted:   2,
		Failed:    1,
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out Entry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out != in {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", out, in)
	}
}

func TestEntry_ZeroValueOmitsGraphCountFields(t *testing.T) {
	data, err := json.Marshal(Entry{Timestamp: time.Unix(0, 0)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)

	for _, key := range []string{`"parsed"`, `"unchanged"`, `"deleted"`, `"failed"`} {
		if strings.Contains(s, key) {
			t.Errorf("zero-value Entry JSON must omit %s, got: %s", key, s)
		}
	}
}

func TestEventConstants(t *testing.T) {
	tests := map[string]string{
		EventLoadSkill:    "load_skill",
		EventMCPCall:      "mcp_call",
		EventHook:         "hook",
		EventGraphRefresh: "graph_refresh",
		EventTddStage:     "tdd_stage",
		EventViolation:    "violation",
		EventMutation:     "mutation",
	}
	for got, want := range tests {
		if got != want {
			t.Errorf("event constant = %q, want %q", got, want)
		}
	}
}
