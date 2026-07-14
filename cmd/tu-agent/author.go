package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// gitAuthor returns the configured git user.email, or "" when unavailable.
// Memory provenance is best-effort: never an error, never a prompt.
func gitAuthor() string {
	out, err := exec.Command("git", "config", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// requireAuthor returns the author for a TEAM-CHUNK write, or a hard error
// with remediation when git user.email is unset. Team chunks are keyed by
// author slug; an empty author collapses every teammate into chunk-local and
// silently overwrites shared notes. Provenance stamping stays best-effort via
// gitAuthor — this hard-error contract applies only at team-chunk write sites.
func requireAuthor(author string) (string, error) {
	if author == "" {
		return "", fmt.Errorf("requireAuthor: git user.email is not set; run `git config user.email you@example.com` before exporting team memory")
	}
	return author, nil
}
