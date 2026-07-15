package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// guardDecision reports whether a PreToolUse call touches a secret/credential
// file, plus the session and tool context needed to record a violation row.
type guardDecision struct {
	touched   bool
	sessionID string
	tool      string
}

// guardFromHook decodes a PreToolUse hook payload and reports whether the call
// touches a secret/credential file — either the Write/Edit target path
// (file_path) or a secret path named in the Bash command — along with the
// session_id and tool_name from the payload. A malformed or empty payload
// yields a zero-value guardDecision (touched=false) so the guard fails open.
//
// On the read side the permissions.deny Read(...) rules are the primary
// protection and this guard is defence in depth. On the write side it is the
// other way round: deny only gates paths a rule spells out, so this guard —
// which resolves the path through IsSecretPath — is the broader net.
func guardFromHook(r io.Reader) guardDecision {
	var payload struct {
		SessionID string `json:"session_id"`
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
			Command  string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return guardDecision{}
	}
	d := guardDecision{sessionID: payload.SessionID, tool: payload.ToolName}
	p := payload.ToolInput.FilePath
	if p == "" {
		p = payload.ToolInput.Path
	}
	if p != "" && codegen.IsSecretPath(p) {
		d.touched = true
		return d
	}
	if c := payload.ToolInput.Command; c != "" &&
		(codegen.CommandTouchesSecret(c) || codegen.CommandExposesEnvSecret(c)) {
		d.touched = true
		return d
	}
	return d
}

// guardPathCmd is the PreToolUse hook target. It reads the hook JSON on stdin
// and exits 2 (block) when the call reads or modifies a secret/credential file.
// It replaces the previous jq-based shell guard, removing the jq dependency.
var guardPathCmd = &cobra.Command{
	Use:    "guard-path",
	Short:  "PreToolUse hook: block Write/Edit/Bash that reads or modifies secret/credential files",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		d := guardFromHook(cmd.InOrStdin())
		if d.touched {
			recordViolation("secret-guard", d.tool, d.sessionID)
			fmt.Fprintln(cmd.ErrOrStderr(), "tu-agent: refusing to read/modify secret files or print environment secrets")
			os.Exit(2)
		}
		return nil
	},
}
