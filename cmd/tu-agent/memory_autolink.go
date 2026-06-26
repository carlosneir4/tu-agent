package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/tu/tu-agent/internal/autolink"
	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/memory"
)

// buildNameIndex maps node name -> node ID for class and file nodes whose name
// is unique within that set. Colliding names are omitted so the matcher only
// ever links unambiguous symbols.
func buildNameIndex(nodes []graph.Node) map[string]string {
	count := make(map[string]int, len(nodes))
	first := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.Kind != graph.KindClass && n.Kind != graph.KindFile {
			continue
		}
		count[n.Name]++
		if count[n.Name] == 1 {
			first[n.Name] = n.ID
		}
	}
	idx := make(map[string]string, len(first))
	for name, id := range first {
		if count[name] == 1 {
			idx[name] = id
		}
	}
	return idx
}

// relinkObservations re-derives documents_auto links for every live observation
// from the symbols it names in prose. Best-effort: a missing or empty graph is a
// no-op, never an error. Idempotent (delete-then-recreate per observation).
func relinkObservations(out io.Writer, quiet bool) error {
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return fmt.Errorf("memory relink: %w", err)
	}
	defer func() {
		if cerr := ms.Close(); cerr != nil {
			slog.Warn("memory relink: store close failed", "err", cerr)
		}
	}()

	gs, err := openGraphStore()
	if err != nil {
		if !quiet {
			fmt.Fprintln(out, "no graph store; nothing to relink")
		}
		return nil
	}
	defer func() { _ = gs.Close() }()

	if n, err := gs.NodeCount(); err != nil || n == 0 {
		if !quiet {
			fmt.Fprintln(out, "graph empty; nothing to relink")
		}
		return nil
	}
	nodes, err := gs.AllNodes()
	if err != nil {
		return fmt.Errorf("memory relink: %w", err)
	}
	index := buildNameIndex(nodes)

	obs, err := ms.List()
	if err != nil {
		return fmt.Errorf("memory relink: %w", err)
	}
	relinked, links := 0, 0
	for _, o := range obs {
		ids := autolink.Resolve(autolink.Symbols(o.Content), index)
		if _, err := ms.DeleteRelationsByType(o.ID, "documents_auto"); err != nil {
			return fmt.Errorf("memory relink: %w", err)
		}
		for _, nodeID := range ids {
			if _, err := ms.Relate(o.ID, nodeID, "documents_auto"); err != nil {
				return fmt.Errorf("memory relink: %w", err)
			}
			links++
		}
		if len(ids) > 0 {
			relinked++
		}
	}
	if !quiet {
		fmt.Fprintf(out, "relinked %d observation(s), %d auto-link(s)\n", relinked, links)
	}
	return nil
}
