package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveConceptRoots returns the concept roots to index, by precedence:
//  1. explicit (flag values ∪ cfg.Learn.ConceptRoots ∪ cfg.Learn.ConceptRoot), deduped;
//  2. else workspace auto-detect (detectConceptRoots);
//  3. else nil → DiscoverConcepts returns nil → domain-map fallback.
func resolveConceptRoots(explicit []string, repoRoot string) []string {
	seen := map[string]bool{}
	var roots []string
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		roots = append(roots, r)
	}
	for _, r := range explicit {
		add(r)
	}
	for _, r := range cfg.Learn.ConceptRoots {
		add(r)
	}
	add(cfg.Learn.ConceptRoot)
	if len(roots) > 0 {
		slog.Info("concepts: explicit roots", "roots", roots)
		return roots
	}
	if auto := detectConceptRoots(repoRoot); len(auto) > 0 {
		slog.Info("concepts: auto-detected roots from workspaces", "roots", auto)
		return auto
	}
	slog.Info("concepts: no roots; domain-map fallback")
	return nil
}

// detectConceptRoots derives concept roots from the repo's package.json
// "workspaces" — the first path segment of each entry (a glob like "packages/*"
// or a path like "rigs/jest" → "packages"/"rigs"), deduped and sorted. It
// handles both the array form and the Yarn object form
// {"packages":[…],"nohoist":[…]}. Returns nil when there is no readable
// package.json or no workspaces, so the caller falls back.
func detectConceptRoots(repoRoot string) []string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || len(pkg.Workspaces) == 0 {
		return nil
	}
	var globs []string
	if err := json.Unmarshal(pkg.Workspaces, &globs); err != nil {
		var obj struct {
			Packages []string `json:"packages"`
		}
		if err := json.Unmarshal(pkg.Workspaces, &obj); err != nil {
			return nil
		}
		globs = obj.Packages
	}
	seen := map[string]bool{}
	var roots []string
	for _, g := range globs {
		g = strings.TrimPrefix(g, "./")
		seg, _, _ := strings.Cut(g, "/")
		if seg == "" || seg == "*" || seen[seg] {
			continue
		}
		seen[seg] = true
		roots = append(roots, seg)
	}
	sort.Strings(roots)
	return roots
}
