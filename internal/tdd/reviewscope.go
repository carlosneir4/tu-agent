package tdd

import (
	"context"
	"strings"
)

// ReviewScope computes the deterministic review scope for the branch at HEAD:
// the merge-base against the repository's default branch and the files that
// changed on the branch since that merge-base.
//
// It detects the default branch in order of preference — origin/HEAD, then
// main, then master — and computes `git merge-base <default> HEAD` as base.
// When the default branch or the merge-base cannot be resolved, or the branch
// diff is empty, it returns a non-empty skipReason and a nil error: an
// unscopable branch is an ordinary, non-fatal outcome, not a failure. An error
// is reserved for genuinely exceptional cases.
func ReviewScope(ctx context.Context, root string) (base string, files []string, skipReason string, err error) {
	def, ok := defaultBranch(ctx, root)
	if !ok {
		return "", nil, "no default branch (origin/HEAD, main, master) found: cannot compute merge-base", nil
	}

	out, mbErr := git(ctx, root, "merge-base", def, "HEAD")
	if mbErr != nil {
		return "", nil, "could not resolve merge-base against " + def, nil
	}
	base = strings.TrimSpace(out)
	if base == "" {
		return "", nil, "merge-base against " + def + " resolved to an empty commit", nil
	}

	files, err = DiffFiles(ctx, root, base, "HEAD")
	if err != nil {
		return "", nil, "", err
	}
	if len(files) == 0 {
		return "", nil, "branch diff is empty: no changes since the merge-base", nil
	}

	return base, files, "", nil
}

// defaultBranch resolves the repository's default branch, preferring the
// remote's origin/HEAD symref, then a local main, then master. It returns the
// ref name usable as a merge-base argument and whether one was found.
func defaultBranch(ctx context.Context, root string) (string, bool) {
	if out, err := git(ctx, root, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref, true
		}
	}
	for _, ref := range []string{"main", "master"} {
		if _, err := git(ctx, root, "rev-parse", "--verify", "-q", ref); err == nil {
			return ref, true
		}
	}
	return "", false
}
