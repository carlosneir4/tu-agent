package tdd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

// TddStatePath resolves the state.json for the addressed run: by ticket, else
// the newest run, else a legacy flat run. Falls back to the flat path so a
// fresh `state begin` has somewhere to write.
func TddStatePath(root, ticket string) string {
	if base, ok := ResolveTddBase(root, ticket); ok {
		return filepath.Join(base, "state.json")
	}
	return filepath.Join(tddDir(root), "state.json")
}

// TddStateFile resolves the state.json for state/status commands: an explicit
// --base wins (relative is joined to root); otherwise falls back to
// --ticket/mtime resolution via TddStatePath.
func TddStateFile(root, baseFlag, ticket string) string {
	if baseFlag != "" {
		b := baseFlag
		if !filepath.IsAbs(b) {
			b = filepath.Join(root, filepath.FromSlash(b))
		}
		return filepath.Join(b, "state.json")
	}
	return TddStatePath(root, ticket)
}

// TddStateBaseRel is the repo-relative dir that holds the resolved state.json.
func TddStateBaseRel(root, statePath string) string {
	rel, err := filepath.Rel(root, filepath.Dir(statePath))
	if err != nil {
		return ""
	}
	return rel
}

// RunStatus prints the current tdd run state as JSON (features, statuses,
// resumable) to out.
func RunStatus(root, baseFlag, ticket string, out io.Writer) error {
	sp := TddStateFile(root, baseFlag, ticket)
	st, err := LoadState(sp)
	if err != nil {
		return fmt.Errorf("tdd status: %w", err)
	}
	outState := struct {
		Task      string         `json:"task"`
		Branch    string         `json:"branch,omitempty"`
		Base      string         `json:"base"`
		Resumable bool           `json:"resumable"`
		Review    string         `json:"review,omitempty"`
		Features  []FeatureState `json:"features"`
	}{st.Task, st.Branch, TddStateBaseRel(root, sp), st.Resumable(), st.Review, st.Features}
	b, err := json.MarshalIndent(outState, "", "  ")
	if err != nil {
		return fmt.Errorf("tdd status: %w", err)
	}
	fmt.Fprintln(out, string(b))
	return nil
}

// oldConductorWarning is printed on a begin whose --complexity flag was
// omitted entirely: a conductor predating the flag never sends it, so an
// unset flag is treated as that legacy caller rather than as "standard"
// fail-closed — it proceeds without a token, but loudly, so a plugin still on
// the old flow gets told to update.
const oldConductorWarning = "warning: no --complexity given — old conductor flow detected; design-approval enforcement skipped. Update the tu-agent plugin for the new conductor flow."

// RunStateBegin starts a fresh run state with the given features all
// pending. complexity gates the begin per the fail-closed matrix: "trivial"
// never requires a prior approval token; "standard"/"complex" require one
// under base, refusing before anything is written; "" (the flag entirely
// omitted) is the old-conductor compat path — it proceeds without a token but
// prints oldConductorWarning; any other value is rejected outright. A
// non-empty complexity is recorded on the returned state; "" leaves it
// omitted.
func RunStateBegin(root, baseFlag, ticket, task, branch string, features []string, complexity string, out io.Writer) error {
	targetPath := TddStateFile(root, baseFlag, ticket)
	oldConductorCompat := false
	switch complexity {
	case "trivial":
		// No plan to approve — never requires a token.
	case "standard", "complex":
		if !designApproved(filepath.Dir(targetPath)) {
			return fmt.Errorf("tdd state begin: design not approved — run `tdd state approve-design` first")
		}
	case "":
		oldConductorCompat = true
	default:
		return fmt.Errorf("tdd state begin: complexity must be trivial|standard|complex, got %q", complexity)
	}
	if len(features) == 0 {
		return fmt.Errorf("tdd state begin: at least one --feature is required")
	}
	feats := make([]FeaturePlan, 0, len(features))
	for _, name := range features {
		feats = append(feats, FeaturePlan{Name: name})
	}
	st, err := BeginRun(task, branch, feats)
	if err != nil {
		return fmt.Errorf("tdd state begin: %w", err)
	}
	if complexity != "" {
		st.Complexity = complexity
	}
	if baseFlag == "" && ticket == "" {
		if existing, loadErr := LoadState(targetPath); loadErr == nil && existing.Resumable() {
			dir := TddStateBaseRel(root, targetPath)
			if dir == "" {
				dir = filepath.Dir(targetPath)
			}
			return fmt.Errorf("tdd state begin: %s has an in-progress run (task %q); pass an explicit --base/--ticket to start a separate run, or finish/mark the current one first", dir, existing.Task)
		}
	}
	if err := SaveState(targetPath, st); err != nil {
		return fmt.Errorf("tdd state begin: %w", err)
	}
	if oldConductorCompat {
		fmt.Fprintln(out, oldConductorWarning)
	}
	fmt.Fprintf(out, "began run with %d feature(s)\n", len(features))
	return nil
}

// RunStateMark marks a feature's terminal status in the run state.
func RunStateMark(root, baseFlag, ticket, name, status string, out io.Writer) error {
	if status != "pass" && status != "blocked" && status != "pending" {
		return fmt.Errorf("tdd state mark: status must be pass|blocked|pending, got %q", status)
	}
	st, err := LoadState(TddStateFile(root, baseFlag, ticket))
	if err != nil {
		return fmt.Errorf("tdd state mark: %w", err)
	}
	known := false
	for _, f := range st.Features {
		if f.Name == name {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("tdd state mark: unknown feature %q", name)
	}
	st.Mark(name, status)
	if err := SaveState(TddStateFile(root, baseFlag, ticket), st); err != nil {
		return fmt.Errorf("tdd state mark: %w", err)
	}
	fmt.Fprintf(out, "marked %s %s\n", name, status)
	return nil
}

// RunStateReview sets the design review gate status in the run state.
func RunStateReview(root, baseFlag, ticket, review string, out io.Writer) error {
	if review != "pending" && review != "pass" && review != "skipped" {
		return fmt.Errorf("tdd state review: value must be pending|pass|skipped, got %q", review)
	}
	st, err := LoadState(TddStateFile(root, baseFlag, ticket))
	if err != nil {
		return fmt.Errorf("tdd state review: %w", err)
	}
	st.Review = review
	if err := SaveState(TddStateFile(root, baseFlag, ticket), st); err != nil {
		return fmt.Errorf("tdd state review: %w", err)
	}
	fmt.Fprintf(out, "review %s\n", review)
	return nil
}
