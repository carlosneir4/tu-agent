package coverage

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseIstanbul parses Istanbul's coverage-final.json (vitest/jest --coverage).
// A statement's lines are covered when its hit count (s[id]) > 0. Absolute file
// paths are made repo-relative against repoRoot; remaining mismatches are
// suffix-matched by SymbolCoverage.
func ParseIstanbul(r io.Reader, repoRoot string) (Profile, error) {
	var raw map[string]struct {
		Path         string `json:"path"`
		StatementMap map[string]struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
			End struct {
				Line int `json:"line"`
			} `json:"end"`
		} `json:"statementMap"`
		S map[string]int `json:"s"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("coverage.ParseIstanbul: %w", err)
	}
	p := Profile{}
	for key, fc := range raw {
		file := fc.Path
		if file == "" {
			file = key
		}
		if repoRoot != "" {
			file = strings.TrimPrefix(file, repoRoot+"/")
		}
		for id, stmt := range fc.StatementMap {
			covered := fc.S[id] > 0
			for ln := stmt.Start.Line; ln <= stmt.End.Line; ln++ {
				p.add(file, ln, covered)
			}
		}
	}
	return p, nil
}
