package mutation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type tsEngine struct{}

func (tsEngine) Name() string { return "stryker" }

// tsNearestPackage walks up from repoRoot/pkgDir to the nearest directory
// containing a package.json, bounded by repoRoot. Returns that absolute dir and
// true, or (repoRoot/pkgDir, false) when none is found on the chain.
func tsNearestPackage(repoRoot, pkgDir string) (string, bool) {
	abs := filepath.Join(repoRoot, pkgDir)
	cleanRoot := filepath.Clean(repoRoot)
	for {
		if _, err := os.Stat(filepath.Join(abs, "package.json")); err == nil {
			return abs, true
		}
		if filepath.Clean(abs) == cleanRoot {
			return filepath.Join(repoRoot, pkgDir), false
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return filepath.Join(repoRoot, pkgDir), false
		}
		abs = parent
	}
}

func (tsEngine) Available(repoRoot, pkgDir string) bool {
	_, ok := tsNearestPackage(repoRoot, pkgDir)
	return ok
}

func (tsEngine) WorkDir(repoRoot, pkgDir string) string {
	dir, _ := tsNearestPackage(repoRoot, pkgDir)
	return dir
}

func (tsEngine) Command(_, _ string) []string {
	return []string{"npx", "stryker", "run"}
}

func (tsEngine) ReportPath(_, _ string) string { return "" }

type strykerReport struct {
	Files map[string]struct {
		Mutants []struct {
			MutatorName string `json:"mutatorName"`
			Status      string `json:"status"`
			Location    struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"location"`
		} `json:"mutants"`
	} `json:"files"`
}

func (tsEngine) Parse(output string) (Report, error) {
	var doc strykerReport
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		return Report{}, fmt.Errorf("tsEngine.Parse: %w", err)
	}
	var rep Report
	// Deterministic file order for stable survivor lists.
	files := make([]string, 0, len(doc.Files))
	for f := range doc.Files {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		for _, m := range doc.Files[f].Mutants {
			rep.Total++
			if m.Status == "Killed" || m.Status == "Timeout" {
				rep.Killed++
			} else {
				rep.Survived++
				rep.Survivors = append(rep.Survivors, Survivor{File: f, Line: m.Location.Start.Line, Desc: m.MutatorName})
			}
		}
	}
	if rep.Total > 0 {
		rep.Score = float64(rep.Killed) / float64(rep.Total)
	}
	return rep, nil
}
