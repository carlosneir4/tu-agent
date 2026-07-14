package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// hookCmd groups Claude Code hook entry points that are invoked from
// plugin/hooks/hooks.json (or the standalone-harden settings.json emitted by
// internal/codegen.hardenHooks), not directly by a user.
var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Claude Code hook entry points (invoked by hooks.json, not directly)",
	Hidden: true,
}

// promptSubmitCmd is the UserPromptSubmit hook target. It records one
// telemetry row per user prompt — a friction proxy: stats --insights counts
// prompts and distinct sessions from it.
var promptSubmitCmd = &cobra.Command{
	Use:    "prompt-submit",
	Short:  "UserPromptSubmit hook: record a prompt row (full telemetry level only)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return promptSubmitDecision(cmd.InOrStdin())
	},
}

// promptSubmitDecision reads a UserPromptSubmit hook payload from r and
// records a prompt telemetry row. Full-tier gated: at any level other than
// "full" it returns immediately without reading r. Never fails the hook: an
// empty or unparseable payload is a silent no-op.
func promptSubmitDecision(r io.Reader) error {
	if telemetryLevel() != "full" {
		return nil
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp: time.Now(),
		Event:     telemetry.EventPrompt,
		SessionID: payload.SessionID,
	})
	return nil
}

func init() {
	hookCmd.AddCommand(promptSubmitCmd)
}
