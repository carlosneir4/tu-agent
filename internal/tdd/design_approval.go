package tdd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// designApprovalToken is the durable record proving a human approved the
// design at plan time — the shape written to $BASE/progress/design-approval.json.
type designApprovalToken struct {
	ApprovedBy string `json:"approved_by"`
	ApprovedAt string `json:"approved_at"`
	Rounds     int    `json:"rounds"`
}

// approvalTokenPath is where the design-approval token lives for a given
// per-run base dir, alongside red-baseline.json.
func approvalTokenPath(base string) string {
	return filepath.Join(base, "progress", "design-approval.json")
}

// designApproved reports whether a valid approval token exists under base:
// present, parseable, and carrying a non-empty approver. An audit token
// without identity is no approval (mirrors requireAuthor's contract) — a
// token that parses but has an empty approved_by does not satisfy the gate.
// Shared by begin's fail-closed matrix and the RED gate's warning.
func designApproved(base string) bool {
	data, err := os.ReadFile(approvalTokenPath(base))
	if err != nil {
		return false
	}
	var tok designApprovalToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return false
	}
	return tok.ApprovedBy != ""
}

// RunStateApproveDesign records a durable approval token (approver, UTC
// timestamp, design rounds) under base and, when $BASE/plan.md exists, stamps
// its "## Sign-off" section. Approval precedes state.json's existence — base
// resolution therefore must not require state.json to already be on disk.
// Re-running overwrites the token (a design loop-back re-approval updates
// identity/timestamp/rounds, never accumulates).
func RunStateApproveDesign(root, baseFlag, ticket string, rounds int, approver string, out io.Writer) error {
	base := filepath.Dir(TddStateFile(root, baseFlag, ticket))
	tok := designApprovalToken{
		ApprovedBy: approver,
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
		Rounds:     rounds,
	}
	if err := os.MkdirAll(filepath.Join(base, "progress"), 0o755); err != nil {
		return fmt.Errorf("tdd state approve-design: %w", err)
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("tdd state approve-design: %w", err)
	}
	if err := os.WriteFile(approvalTokenPath(base), data, 0o644); err != nil {
		return fmt.Errorf("tdd state approve-design: %w", err)
	}
	if err := stampSignOff(base, approver, tok.ApprovedAt, rounds); err != nil {
		return fmt.Errorf("tdd state approve-design: %w", err)
	}
	fmt.Fprintf(out, "design approved by %s (round %d)\n", approver, rounds)
	return nil
}

// stampSignOff writes or replaces base/plan.md's "## Sign-off" section with
// the deterministic approval line. A missing plan.md is skipped silently —
// the trivial and legacy paths have no plan.md to stamp.
func stampSignOff(base, approver, approvedAt string, rounds int) error {
	planPath := filepath.Join(base, "plan.md")
	data, err := os.ReadFile(planPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stampSignOff: %w", err)
	}
	section := fmt.Sprintf("## Sign-off\n\nApproved by %s on %s after %d design round(s).\n", approver, approvedAt, rounds)
	updated := replaceOrAppendSection(string(data), "## Sign-off", section)
	if err := os.WriteFile(planPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("stampSignOff: %w", err)
	}
	return nil
}

// replaceOrAppendSection substitutes content's existing heading section (from
// the line that is EXACTLY heading, tolerating a trailing \r, through the
// next line starting with "## " or EOF) with section, or appends section as a
// new trailing section when no line matches heading exactly. An exact-line
// match — never a prefix match — so "## Sign-off notes" (a different,
// similarly-named section) and "### Sign-off" (an h3 subsection) are never
// mistaken for the h2 heading. Re-approval therefore replaces, never
// duplicates, the section.
func replaceOrAppendSection(content, heading, section string) string {
	lines := strings.Split(content, "\n")
	headIdx := -1
	for i, line := range lines {
		if strings.TrimRight(line, "\r") == heading {
			headIdx = i
			break
		}
	}
	if headIdx == -1 {
		trimmed := strings.TrimRight(content, "\n")
		if trimmed == "" {
			return section
		}
		return trimmed + "\n\n" + section
	}
	endIdx := len(lines)
	for i := headIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimRight(lines[i], "\r"), "## ") {
			endIdx = i
			break
		}
	}
	newLines := strings.Split(strings.TrimRight(section, "\n"), "\n")
	result := make([]string, 0, headIdx+len(newLines)+(len(lines)-endIdx))
	result = append(result, lines[:headIdx]...)
	result = append(result, newLines...)
	result = append(result, lines[endIdx:]...)
	return strings.Join(result, "\n")
}

// designApprovalWarning returns a non-blocking warning when base has a
// begun, non-trivial run with no approval token, or "" when nothing warrants
// one. A missing/empty state.json (nothing begun yet) never warns — every
// existing gate test relies on that silence.
func designApprovalWarning(base string) string {
	st, err := LoadState(filepath.Join(base, "state.json"))
	if err != nil || len(st.Features) == 0 {
		return ""
	}
	if st.Complexity == "trivial" {
		return ""
	}
	if designApproved(base) {
		return ""
	}
	return "design not approved for this run — run `tdd state approve-design --base <base>`"
}
