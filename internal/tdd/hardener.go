package tdd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// MutationTarget is the package the gate mutates, resolved from the craftsman's
// reported source artifact.
type MutationTarget struct {
	Language string // mutation engine key: go | java | python | typescript
	Dir      string // package directory to mutate
}

// MutationOutcome is the gate-relevant projection of a mutation report, kept
// local so internal/tdd does not depend on internal/mutation.
type MutationOutcome struct {
	Skipped   bool
	Score     float64 // 0..1 kill ratio
	Survivors []string
	Note      string
}

// Mutator runs mutation testing for one target. Injected so the conductor stays
// testable with fakes; the real implementation lives in cmd and wraps
// mutation.Run. It must never panic.
type Mutator func(ctx context.Context, t MutationTarget) MutationOutcome

// MutationTargetFromContract resolves the gate's target from the craftsman's
// first source artifact (Kind=="source"): language from the file extension,
// directory from filepath.Dir. Returns false when there is no source artifact
// or the first one's extension is unknown — the caller then skips the gate.
func MutationTargetFromContract(c Contract) (MutationTarget, bool) {
	for _, a := range c.Artifacts {
		if a.Kind != "source" {
			continue
		}
		return MutationTargetFromArtifact(a)
	}
	return MutationTarget{}, false
}

// MutationTargetFromArtifact resolves a mutation target from a single source
// artifact: language from the extension, directory from the path. Returns false
// for a non-source artifact, an empty path, or an unknown extension.
func MutationTargetFromArtifact(a Artifact) (MutationTarget, bool) {
	if a.Kind != "source" || a.Path == "" {
		return MutationTarget{}, false
	}
	lang, ok := languageFromExt(filepath.Ext(a.Path))
	if !ok {
		return MutationTarget{}, false
	}
	return MutationTarget{Language: lang, Dir: filepath.Dir(a.Path)}, true
}

func languageFromExt(ext string) (string, bool) {
	switch ext {
	case ".go":
		return "go", true
	case ".java":
		return "java", true
	case ".py":
		return "python", true
	case ".ts":
		return "typescript", true
	default:
		return "", false
	}
}

// MutationGate interprets an outcome against the threshold, reusing DetResult.
// A skipped outcome is advisory (OK). Score at or above the threshold is OK.
// Below threshold is not OK, with the surviving mutants in Feedback.
func MutationGate(threshold float64, out MutationOutcome) DetResult {
	if out.Skipped {
		return DetResult{OK: true}
	}
	if out.Score >= threshold {
		return DetResult{OK: true}
	}
	fb := fmt.Sprintf("mutation score %.0f%% < %.0f%% — surviving mutants:\n%s",
		out.Score*100, threshold*100, strings.Join(out.Survivors, "\n"))
	return DetResult{Feedback: fb}
}
