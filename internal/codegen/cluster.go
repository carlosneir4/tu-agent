package codegen

import (
	"fmt"
	"sort"

	"github.com/tu/tu-agent/internal/cluster"
)

// WeightedEdge is a coupling between two files with a strength. Direction is
// ignored by clustering; duplicate pairs accumulate.
type WeightedEdge struct {
	From   string
	To     string
	Weight int
}

// ClusterFiles groups file paths into modularity-maximizing communities over
// the undirected weighted file graph. It is a thin string↔index adapter over
// the leaf cluster package: file paths are mapped to node indices, edges are
// folded into cluster.Edge, and the integer communities are mapped back to
// sorted file groups. Pure and deterministic: identical inputs yield identical
// output regardless of edge order. Zero-degree files come back as singleton
// communities. Members of each community are sorted; communities are ordered
// by their first member.
//
// NOTE: this delegates to the leaf package's Louvain local-moving, which
// replaced codegen's earlier CNM agglomerative pair-merging. Different
// algorithms — the partition stays contract-compatible (deterministic, valid,
// order-invariant) but is not guaranteed bit-identical to the pre-refactor
// output; the codegen tests pin the contract, not a specific partition.
func ClusterFiles(files []string, edges []WeightedEdge) [][]string {
	nodes := append([]string(nil), files...)
	sort.Strings(nodes)
	idx := make(map[string]int, len(nodes))
	for i, p := range nodes {
		idx[p] = i
	}

	// Fold weighted file edges into index-based cluster edges, preserving the
	// existing filtering: drop self-edges, unknown endpoints, and non-positive
	// weights. Duplicate pairs accumulate inside cluster.Communities.
	cedges := make([]cluster.Edge, 0, len(edges))
	for _, e := range edges {
		a, okA := idx[e.From]
		b, okB := idx[e.To]
		if !okA || !okB || a == b || e.Weight <= 0 {
			continue
		}
		cedges = append(cedges, cluster.Edge{A: a, B: b, Weight: e.Weight})
	}

	comms := cluster.Communities(len(nodes), cedges)
	out := make([][]string, 0, len(comms))
	for _, c := range comms {
		g := make([]string, 0, len(c))
		for _, i := range c {
			g = append(g, nodes[i])
		}
		out = append(out, g)
	}
	return out
}

// subClusterOversized recursively re-clusters a community larger than maxFiles
// by re-running greedy modularity on the subgraph induced by its own members.
// ClusterFiles scopes its modularity denominator (2m) to only the edges
// internal to the node set it receives, so isolating a sub-community raises the
// effective resolution: genuinely separate sub-modules that a single global
// pass merged (the modularity resolution limit) split apart here. A community
// with no internal seam — a true clique — comes back whole, leaving the
// package/byte guardrail to slice it. Passing the full weighted edge set is
// safe: ClusterFiles ignores edges whose endpoints fall outside community.
//
// maxFiles <= 0 disables sub-clustering; communities at or under maxFiles are
// returned unchanged. Pure and deterministic.
func subClusterOversized(community []string, weighted []WeightedEdge, maxFiles int) [][]string {
	if maxFiles <= 0 || len(community) <= maxFiles {
		return [][]string{community}
	}
	sub := ClusterFiles(community, weighted)
	if len(sub) <= 1 {
		return [][]string{community} // indivisible: no internal seam to split on
	}
	out := make([][]string, 0, len(sub))
	for _, c := range sub {
		out = append(out, subClusterOversized(c, weighted, maxFiles)...)
	}
	return out
}

// communitiesToDomains converts communities into Domains. Each community's
// package is the longest common package prefix of its members (empty falls
// back to the repo root); its name is the kebab rendering of that package
// relative to root. Name collisions get deterministic -2, -3... suffixes in
// community order.
//
// Special case: when a community's package equals root (all files in the repo
// share one package), names are rendered relative to parentPackage(root) so
// that communities get the leaf segment ("app") rather than the uninformative
// "core".
func communitiesToDomains(comms [][]string, units map[string]SourceUnit, root string) []Domain {
	taken := map[string]int{}
	out := make([]Domain, 0, len(comms))
	for _, members := range comms {
		us := make([]SourceUnit, 0, len(members))
		for _, p := range members {
			us = append(us, units[p])
		}
		pkg := commonPackagePrefix(us)
		if pkg == "" {
			pkg = root
		}
		nameRoot := root
		// When every file in the repo shares one package, communities can only
		// be distinguished by topology, not by package. Name them relative to
		// the parent package so they get the leaf segment rather than "core".
		if pkg == root && root != "" {
			nameRoot = parentPackage(root)
		}
		name := kebabPackage(pkg, nameRoot)
		taken[name]++
		if n := taken[name]; n > 1 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		files := append([]string(nil), members...)
		sort.Strings(files)
		out = append(out, Domain{Name: name, Package: pkg, Files: files})
	}
	return out
}

// clusterFallbackMinFiles is the small-repo threshold: repos smaller
// than this use the package-path heuristic instead of topology clustering.
const clusterFallbackMinFiles = 20

// batchOverfullLeaves splits any leaf domain whose file count exceeds maxFiles
// into sequential numbered batches of at most maxFiles each. This is a
// file-count–based fallback for leaf packages where sub-package splitting is
// impossible (all files share the same package with no deeper sub-package).
// Parent markers (d.Files == nil) and domains within the limit are returned
// unchanged. Deterministic: files within each domain are sorted before
// batching.
func batchOverfullLeaves(domains []Domain, maxFiles int) []Domain {
	if maxFiles <= 0 {
		return domains
	}
	out := make([]Domain, 0, len(domains))
	for _, d := range domains {
		if d.Files == nil || len(d.Files) <= maxFiles {
			out = append(out, d)
			continue
		}
		// Emit a parent marker for this domain then numbered leaf batches.
		parent := d.Name
		if d.Parent != "" {
			parent = d.Parent
		}
		if d.Parent == "" {
			out = append(out, Domain{Name: d.Name, Package: d.Package, Files: nil})
		}
		paths := append([]string(nil), d.Files...)
		sort.Strings(paths)
		for i := 0; i < len(paths); i += maxFiles {
			end := i + maxFiles
			if end > len(paths) {
				end = len(paths)
			}
			batch := paths[i:end]
			batchNum := i/maxFiles + 1
			out = append(out, Domain{
				Name:    fmt.Sprintf("%s-%d", d.Name, batchNum),
				Package: d.Package,
				Files:   batch,
				Parent:  parent,
			})
		}
	}
	sortDomains(out)
	return out
}

// BuildDomainMapClustered groups files into domains by graph topology —
// greedy modularity over weighted file edges — then applies the same
// guardrail pipeline as BuildDomainMap (merge tiny, split huge). Falls back
// to BuildDomainMap when the repo has fewer than clusterFallbackMinFiles
// files or no weighted edges. Pure and deterministic.
func BuildDomainMapClustered(files []SourceUnit, importEdges []Edge, weighted []WeightedEdge, opts DomainMapOptions) []Domain {
	if len(files) < clusterFallbackMinFiles || len(weighted) == 0 {
		return BuildDomainMap(files, importEdges, opts)
	}
	if opts.Depth < 1 {
		opts.Depth = 1
	}
	root := commonPackagePrefix(files)
	unitByPath := byPath(files)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	comms := ClusterFiles(paths, weighted)

	// Re-group zero-degree singletons by the package heuristic so unconnected
	// files don't become one domain each.
	degree := map[string]int{}
	for _, e := range weighted {
		if e.From != e.To && e.Weight > 0 {
			degree[e.From]++
			degree[e.To]++
		}
	}
	connected := make([][]string, 0, len(comms))
	loose := map[string][]string{} // domain package -> files
	for _, c := range comms {
		if len(c) == 1 && degree[c[0]] == 0 {
			dp := domainPackage(unitByPath[c[0]].Package, root, opts.Depth)
			loose[dp] = append(loose[dp], c[0])
			continue
		}
		connected = append(connected, c)
	}

	// Recursively re-cluster oversized communities along their internal
	// topology before they become domains, so a dense blob is split by
	// structure rather than sliced mechanically by package path downstream.
	// Loose (zero-degree) package groups carry no edges and are never
	// re-clustered — that would shatter them back into singletons.
	refined := make([][]string, 0, len(comms))
	for _, c := range connected {
		refined = append(refined, subClusterOversized(c, weighted, opts.MaxFiles)...)
	}

	looseKeys := make([]string, 0, len(loose))
	for k := range loose {
		looseKeys = append(looseKeys, k)
	}
	sort.Strings(looseKeys)
	for _, k := range looseKeys {
		sort.Strings(loose[k])
		refined = append(refined, loose[k])
	}

	domains := communitiesToDomains(refined, unitByPath, root)
	sortDomains(domains)
	domains = mergeTinyDomains(domains, importEdges, opts.MinFiles, opts.MinStandaloneFiles)
	domains = splitHugeDomains(domains, unitByPath, opts.MaxFiles, opts.MaxBytes, opts.Depth)
	// File-count fallback: splitHugeDomains cannot batch by file count at leaf
	// packages (no sub-package boundary). Apply a sequential batch pass so no
	// leaf exceeds MaxFiles.
	domains = batchOverfullLeaves(domains, opts.MaxFiles)
	sortDomains(domains)
	return domains
}
