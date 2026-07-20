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

// promptExpansionHookCmd is the UserPromptExpansion hook target. It records
// one telemetry row per slash-command expansion — stats --flow and future
// skill-adoption reporting read it via Entry.Skill. Named distinctly from the
// test file's promptExpansionCmd(t) helper to avoid a package-level clash.
var promptExpansionHookCmd = &cobra.Command{
	Use:    "prompt-expansion",
	Short:  "UserPromptExpansion hook: record a skill_invoked row (full telemetry level only)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return promptExpansionDecision(cmd.InOrStdin())
	},
}

// promptExpansionDecision reads a UserPromptExpansion hook payload from r and
// records a skill_invoked telemetry row. Full-tier gated: at any level other
// than "full" it returns immediately without reading r. Never fails the
// hook: an empty, unparseable, or command_name-less payload (a plain prompt,
// not a slash expansion) is a silent no-op.
func promptExpansionDecision(r io.Reader) error {
	if telemetryLevel() != "full" {
		return nil
	}
	var payload struct {
		SessionID   string `json:"session_id"`
		CommandName string `json:"command_name"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil
	}
	if payload.CommandName == "" {
		return nil
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp: time.Now(),
		Event:     telemetry.EventSkillInvoked,
		Skill:     payload.CommandName,
		SessionID: payload.SessionID,
	})
	return nil
}

// mcpActionCmd is the PostToolUse (mcp__playwright matcher) hook target. It
// records one telemetry row per browser MCP tool call — an audit trail of
// what the agent did in the browser. Unlike prompt-submit/prompt-expansion,
// this is NOT gated by telemetryLevel(): the audit trail is a security
// property, recorded at every level.
var mcpActionCmd = &cobra.Command{
	Use:    "mcp-action",
	Short:  "PostToolUse hook: record an mcp_call row for a browser MCP tool (all telemetry levels)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return mcpActionDecision(cmd.InOrStdin())
	},
}

// mcpActionDecision reads a PostToolUse hook payload from r and records an
// mcp_call telemetry row. Never fails the hook: an empty, unparseable, or
// tool_name-less payload is a silent no-op.
func mcpActionDecision(r io.Reader) error {
	var payload struct {
		SessionID string `json:"session_id"`
		ToolName  string `json:"tool_name"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil
	}
	if payload.ToolName == "" {
		return nil
	}
	logTelemetryEvent(telemetry.Entry{
		Timestamp: time.Now(),
		Event:     telemetry.EventMCPCall,
		Tool:      payload.ToolName,
		SessionID: payload.SessionID,
	})
	return nil
}

func init() {
	hookCmd.AddCommand(promptSubmitCmd)
	hookCmd.AddCommand(promptExpansionHookCmd)
	hookCmd.AddCommand(mcpActionCmd)
}
