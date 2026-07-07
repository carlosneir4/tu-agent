package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

// treeMutatingRe matches a tree-mutating verb at COMMAND position — start of any
// line or right after a shell operator — so arguments like the "rm" in
// `grep rm file` do not false-match. sed only counts with -i (in-place).
var treeMutatingRe = regexp.MustCompile(`(?m)(?:^|[;&|])\s*(?:rm|mv|cp|tee|patch|sed\s+(?:\S+\s+)*-i|git\s+(?:checkout|switch|reset|restore|stash|clean|apply|revert|rm|merge|rebase|pull))\b`)

// srcRedirectRe matches shell redirection straight into a source file — writes
// the graph must reconcile. Bounded to known source extensions so log/tmp
// redirects stay quiet.
var srcRedirectRe = regexp.MustCompile(`>>?\s*\S+\.(?:go|java|py|ts|tsx|js|jsx|kt|rb|rs)\b`)

// mutatesTree reports whether a shell command likely mutated the working tree.
// It errs toward true: a false positive costs one extra (cheap-when-clean)
// reconcile; a false negative leaves a stale graph node until the next reconcile.
func mutatesTree(cmd string) bool {
	return treeMutatingRe.MatchString(cmd) || srcRedirectRe.MatchString(cmd)
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
