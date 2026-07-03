package testresult

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// reportDirs are the standard JUnit XML output directories, relative to a repo
// (or module) root: Gradle test-results and Maven sure/failsafe reports.
var reportDirs = []string{
	filepath.Join("build", "test-results"),
	filepath.Join("target", "surefire-reports"),
	filepath.Join("target", "failsafe-reports"),
}

// DiscoverJUnitReports walks the standard report directories under root and
// returns every *.xml path found. Missing directories are skipped, not errors.
func DiscoverJUnitReports(root string) ([]string, error) {
	var out []string
	for _, rel := range reportDirs {
		base := filepath.Join(root, rel)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".xml") {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("testresult.DiscoverJUnitReports: %w", err)
		}
	}
	return out, nil
}

// LoadReports parses and merges every discovered report modified at or after
// since. A zero since includes all reports.
func LoadReports(root string, since time.Time) (Report, error) {
	paths, err := DiscoverJUnitReports(root)
	if err != nil {
		return Report{}, err
	}
	var merged Report
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return Report{}, fmt.Errorf("testresult.LoadReports: stat %s: %w", p, err)
		}
		if !since.IsZero() && info.ModTime().Before(since) {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return Report{}, fmt.Errorf("testresult.LoadReports: open %s: %w", p, err)
		}
		rep, perr := ParseJUnitXML(f)
		_ = f.Close()
		if perr != nil {
			return Report{}, fmt.Errorf("testresult.LoadReports: %s: %w", p, perr)
		}
		merged.Merge(rep)
	}
	return merged, nil
}
