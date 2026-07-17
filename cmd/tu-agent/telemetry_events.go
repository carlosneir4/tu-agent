package main

import (
	"time"

	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// telemetryLevel returns the effective telemetry level, defaulting to
// "minimal" for any value other than "full" (including empty/unset).
func telemetryLevel() string {
	if cfg.Telemetry.Level == "full" {
		return "full"
	}
	return "minimal"
}

// logTelemetryEvent best-effort appends one event row to the repo telemetry
// log. Failures to open the log are swallowed — telemetry must never break a
// command.
func logTelemetryEvent(e telemetry.Entry) {
	lg, err := telemetry.NewLogger(telemetryPath(repoRoot()))
	if err != nil {
		return
	}
	_ = lg.Log(e)
}

// recordGraphRefresh records a graph_refresh row. Recorded only at full
// telemetry level.
func recordGraphRefresh(res extract.BuildResult, dur time.Duration) {
	if telemetryLevel() != "full" {
		return
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp:  time.Now(),
		Event:      telemetry.EventGraphRefresh,
		DurationMS: dur.Milliseconds(),
		OK:         res.Failed == 0,
		Parsed:     res.Parsed,
		Unchanged:  res.Unchanged,
		Deleted:    res.Deleted,
		Failed:     res.Failed,
	})
}

// recordViolation records a violation row (secret-guard block,
// edit-without-context, ...). Recorded only at full telemetry level.
func recordViolation(outcome, tool, sessionID string) {
	if telemetryLevel() != "full" {
		return
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp: time.Now(),
		Event:     telemetry.EventViolation,
		Outcome:   outcome,
		Tool:      tool,
		SessionID: sessionID,
	})
}

// recordHook records a hook invocation row. At minimal level only failures
// are recorded (so a swallowed `|| exit 0` failure is still visible); at
// full level, every invocation is recorded.
func recordHook(name string, dur time.Duration, err error) {
	if telemetryLevel() != "full" && err == nil {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp:  time.Now(),
		Event:      telemetry.EventHook,
		Tool:       name,
		DurationMS: dur.Milliseconds(),
		OK:         err == nil,
		Error:      msg,
	})
}
