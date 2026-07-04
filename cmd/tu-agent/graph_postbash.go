package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

// treeMutatingRe matches a tree-mutating verb only at COMMAND position — the
// start of the command or right after a shell operator (; & | ) — so an argument
// like the "rm" in `grep rm file` does not false-match. Covers deletes/moves/
// patches and git operations that change tracked files.
var treeMutatingRe = regexp.MustCompile(`(?:^|[;&|])\s*(?:rm|mv|patch|git\s+(?:checkout|switch|reset|restore|stash|clean|apply|revert|rm|merge|rebase|pull))\b`)

// mutatesTree reports whether a shell command likely mutated the working tree.
// It errs toward true: a false positive costs one extra (cheap-when-clean)
// reconcile; a false negative leaves a stale graph node until the next reconcile.
func mutatesTree(cmd string) bool {
	return treeMutatingRe.MatchString(cmd)
}

// postBashDecision reads a PostToolUse payload (JSON) from r and calls reconcile
// iff the tool's command mutated the tree. Empty or unparseable input is a no-op
// (nil): the hook must never fail the user's Bash command.
func postBashDecision(r io.Reader, reconcile func() error) error {
	data, err := io.ReadAll(r)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var payload struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	if mutatesTree(payload.ToolInput.Command) {
		if err := reconcile(); err != nil {
			// Never fail the hook: a reconcile failure must not break the user's Bash command.
			fmt.Fprintf(os.Stderr, "tu-agent graph update --post-bash: reconcile failed: %v\n", err)
		}
	}
	return nil
}
