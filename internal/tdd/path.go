package tdd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Slugify turns a feature description into a stable kebab slug: lower-case,
// non-alphanumeric runs collapse to a single '-', first 5 words, max 40 chars.
// Degenerate input yields "feature".
func Slugify(desc string) string {
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

// SanitizeTicket keeps path-safe ticket characters and preserves case.
func SanitizeTicket(ticket string) string {
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

// TddRelBase is the repo-relative per-feature artifact dir.
func TddRelBase(ticket, slug string) string {
	if t := SanitizeTicket(ticket); t != "" {
		return path.Join(".tu-agent", "tdd", t+"-"+slug)
	}
	return path.Join(".tu-agent", "tdd", slug)
}

// CurrentBranch returns the checked-out branch name, or "" on error.
func CurrentBranch(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// WarnBranch prints an advisory warning (never mutates git) when a ticket is
// given and the current branch name does not reference it.
func WarnBranch(current, ticket string, w io.Writer) {
	if ticket == "" || current == "" {
		return
	}
	if !strings.Contains(current, ticket) {
		fmt.Fprintf(w, "⚠ not on a branch for ticket %s (current branch: %s)\n", ticket, current)
	}
}

// ResolveTddBase finds the base dir to read for status/gate/resume. With a
// ticket it prefers a matching subdir; otherwise the newest subdir by mtime;
// falling back to the legacy flat dir when it holds a state.json.
func ResolveTddBase(root, ticket string) (string, bool) {
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
	sanitized := SanitizeTicket(ticket)
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

// ResolveTddBaseForFeature resolves the per-feature base dir for a specific
// feature, so that separate RED and GREEN gate invocations agree on the same
// dir even if an unrelated run dir with a newer mtime appears in between (see
// ResolveTddBase's mtime-based resolution, which is not stable across that
// window). It prefers the candidate dir that already holds
// features/<feature>.feature — written once at spec time and stable across
// the RED/GREEN window — falling back to ResolveTddBase's newest-by-mtime
// resolution when no candidate contains that file yet (e.g. the first RED
// call for this feature, before any dir holds it).
func ResolveTddBaseForFeature(root, ticket, feature string) (string, bool) {
	tddDir := filepath.Join(root, ".tu-agent", "tdd")
	entries, err := os.ReadDir(tddDir)
	if err != nil {
		return ResolveTddBase(root, ticket)
	}
	sanitized := SanitizeTicket(ticket)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "features" || e.Name() == "progress" {
			continue
		}
		if sanitized != "" && e.Name() != sanitized && !strings.HasPrefix(e.Name(), sanitized+"-") {
			continue
		}
		cand := filepath.Join(tddDir, e.Name())
		if _, ferr := os.Stat(filepath.Join(cand, "features", feature+".feature")); ferr == nil {
			return cand, true
		}
	}
	return ResolveTddBase(root, ticket)
}

// PromptRelBase picks the per-feature base dir for a stage prompt: an explicit
// --base wins (used by the plugin, which resolves $BASE once); otherwise it is
// derived from the ticket + feature description.
func PromptRelBase(base, ticket string, descArgs []string) string {
	if base != "" {
		return base
	}
	return TddRelBase(ticket, Slugify(strings.Join(descArgs, " ")))
}
