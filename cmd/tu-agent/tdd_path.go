package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// slugify turns a feature description into a stable kebab slug: lower-case,
// non-alphanumeric runs collapse to a single '-', first 5 words, max 40 chars.
// Degenerate input yields "feature".
func slugify(desc string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(desc) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "feature"
	}
	// If > 40 chars, truncate first
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
		if s == "" {
			return "feature"
		}
		return s
	}
	// Otherwise apply 5-word limit
	words := strings.Split(s, "-")
	if len(words) > 5 {
		words = words[:5]
		s = strings.Join(words, "-")
	}
	if s == "" {
		return "feature"
	}
	return s
}

// sanitizeTicket keeps path-safe ticket characters and preserves case.
func sanitizeTicket(ticket string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range ticket {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// tddRelBase is the repo-relative per-feature artifact dir.
func tddRelBase(ticket, slug string) string {
	if t := sanitizeTicket(ticket); t != "" {
		return path.Join(".tu-agent", "tdd", t+"-"+slug)
	}
	return path.Join(".tu-agent", "tdd", slug)
}

// tddBaseDir is the absolute per-feature artifact dir under root.
func tddBaseDir(root, ticket, slug string) string {
	return filepath.Join(root, tddRelBase(ticket, slug))
}

// currentBranch returns the checked-out branch name, or "" on error.
func currentBranch(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// warnBranch prints an advisory warning (never mutates git) when a ticket is
// given and the current branch name does not reference it.
func warnBranch(current, ticket string, w io.Writer) {
	if ticket == "" || current == "" {
		return
	}
	if !strings.Contains(current, ticket) {
		fmt.Fprintf(w, "⚠ not on a branch for ticket %s (current branch: %s)\n", ticket, current)
	}
}

// resolveTddBase finds the base dir to read for status/gate/resume. With a
// ticket it prefers a matching subdir; otherwise the newest subdir by mtime;
// falling back to the legacy flat dir when it holds a state.json.
func resolveTddBase(root, ticket string) (string, bool) {
	tddDir := filepath.Join(root, ".tu-agent", "tdd")
	entries, err := os.ReadDir(tddDir)
	if err != nil {
		return "", false
	}
	type cand struct {
		path string
		mod  int64
	}
	var subs []cand
	sanitized := sanitizeTicket(ticket)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// "features" and "progress" are the legacy flat layout's own artifact
		// subdirs, never a per-ticket/slug run dir — treating either as a run
		// dir would shadow the flat fallback below for repos on the old layout.
		if e.Name() == "features" || e.Name() == "progress" {
			continue
		}
		if sanitized != "" && e.Name() != sanitized && !strings.HasPrefix(e.Name(), sanitized+"-") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		subs = append(subs, cand{filepath.Join(tddDir, e.Name()), info.ModTime().UnixNano()})
	}
	if len(subs) > 0 {
		sort.Slice(subs, func(i, j int) bool { return subs[i].mod > subs[j].mod })
		return subs[0].path, true
	}
	if sanitized == "" {
		if _, serr := os.Stat(filepath.Join(tddDir, "state.json")); serr == nil {
			return tddDir, true
		}
	}
	return "", false
}

var tddPathTicket string

var tddPathCmd = &cobra.Command{
	Use:   "path [feature description...]",
	Short: "Print the repo-relative per-feature artifact dir for a tdd run",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := slugify(strings.Join(args, " "))
		fmt.Fprintln(cmd.OutOrStdout(), tddRelBase(tddPathTicket, slug))
		return nil
	},
}

func init() {
	tddPathCmd.Flags().StringVar(&tddPathTicket, "ticket", "", "ticket id; groups artifacts under <ticket>-<slug>")
	tddCmd.AddCommand(tddPathCmd)
}
