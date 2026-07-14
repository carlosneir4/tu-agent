package main

import "github.com/carlosneir4/tu-agent/internal/memory"

// nodeChecker is the slice of the graph store that staleNodeRefs needs. Defined
// here (the consumer) so the memory subsystem keeps no graph dependency and the
// helper is unit-testable with a fake. *graph/store.Store satisfies it.
type nodeChecker interface {
	NodeCount() (int, error)
	ExistingNodeIDs(ids []string) (map[string]bool, error)
}

// staleNodeRefs returns, per observation ID, how many of its outgoing graph-node
// relations point at a node that no longer exists. Relations to other
// observations (obs↔obs links) are ignored. Degrades to nil when the graph is
// unavailable or unbuilt so recall never breaks or false-flags.
func staleNodeRefs(memStore *memory.Store, gs nodeChecker, obs []memory.Observation) map[string]int {
	if gs == nil || len(obs) == 0 {
		return nil
	}
	obsIDs := make([]string, len(obs))
	for i, o := range obs {
		obsIDs[i] = o.ID
	}
	rels, err := memStore.RelationsFrom(obsIDs)
	if err != nil || len(rels) == 0 {
		return nil
	}
	toIDs := make([]string, 0, len(rels))
	for _, r := range rels {
		toIDs = append(toIDs, r.ToID)
	}
	// Skip targets that are themselves observations.
	obsTargets, err := memStore.ObservationsByID(toIDs)
	if err != nil {
		return nil
	}
	isObs := make(map[string]bool, len(obsTargets))
	for _, o := range obsTargets {
		isObs[o.ID] = true
	}
	seen := make(map[string]bool)
	candidates := make([]string, 0, len(toIDs))
	for _, id := range toIDs {
		if !isObs[id] && !seen[id] {
			candidates = append(candidates, id)
			seen[id] = true
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Guard: an empty/unbuilt graph would make every candidate look missing.
	if n, err := gs.NodeCount(); err != nil || n == 0 {
		return nil
	}
	existing, err := gs.ExistingNodeIDs(candidates)
	if err != nil {
		return nil
	}
	stale := make(map[string]int)
	for _, r := range rels {
		if !isObs[r.ToID] && !existing[r.ToID] {
			stale[r.FromID]++
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return stale
}

// recallStale opens the graph store and computes staleNodeRefs for a recall
// result. It short-circuits before opening the graph store when no observation
// carries relations (the common case), keeping recall latency low. Any failure
// degrades to nil.
func recallStale(memStore *memory.Store, obs []memory.Observation) map[string]int {
	if len(obs) == 0 {
		return nil
	}
	ids := make([]string, len(obs))
	for i, o := range obs {
		ids[i] = o.ID
	}
	rels, err := memStore.RelationsFrom(ids)
	if err != nil || len(rels) == 0 {
		return nil
	}
	gs, err := openGraphStore()
	if err != nil {
		return nil
	}
	defer func() { _ = gs.Close() }()
	return staleNodeRefs(memStore, gs, obs)
}
