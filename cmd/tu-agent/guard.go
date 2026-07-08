package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/codegen"
)

// guardFromHook decodes a PreToolUse hook payload and reports whether the call
// touches a secret/credential file — either the Write/Edit target path
// (file_path) or a secret path named in the Bash command. A malformed or empty
// payload yields false so the guard fails open (the permissions.deny rules
// remain the primary protection).
func guardFromHook(r io.Reader) bool {
	var payload struct {
		ToolInput struct {
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
			Command  string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return false
	}
	p := payload.ToolInput.FilePath
	if p == "" {
		p = payload.ToolInput.Path
	}
	if p != "" && codegen.IsSecretPath(p) {
		return true
	}
	if c := payload.ToolInput.Command; c != "" &&
		(codegen.CommandTouchesSecret(c) || codegen.CommandExposesEnvSecret(c)) {
		return true
	}
	return false
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
		if guardFromHook(cmd.InOrStdin()) {
			fmt.Fprintln(cmd.ErrOrStderr(), "tu-agent: refusing to read/modify secret files or print environment secrets")
			os.Exit(2)
		}
		return nil
	},
}
