package main

import (
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
