package codegen

import (
	"fmt"
	"sort"
)

// WeightedEdge is a coupling between two files with a strength. Direction is
// ignored by clustering; duplicate pairs accumulate.
type WeightedEdge struct {
	From   string
	To     string
	Weight int
}

// ClusterFiles groups file paths into communities by greedy modularity
// maximization (Leiden-inspired agglomerative merging) over the undirected
// weighted file graph. Pure and deterministic: identical inputs yield
// identical output regardless of edge order. Zero-degree files come back as
// singleton communities. Members of each community are sorted; communities
// are ordered by their first member.
func ClusterFiles(files []string, edges []WeightedEdge) [][]string {
	nodes := append([]string(nil), files...)
	sort.Strings(nodes)
	idx := make(map[string]int, len(nodes))
	for i, p := range nodes {
		idx[p] = i
	}

	// Node-level undirected weights, key always (low, high).
	w := map[[2]int]float64{}
	for _, e := range edges {
		a, okA := idx[e.From]
		b, okB := idx[e.To]
		if !okA || !okB || a == b || e.Weight <= 0 {
			continue
		}
		if a > b {
			a, b = b, a
		}
		w[[2]int{a, b}] += float64(e.Weight)
	}
	deg := make([]float64, len(nodes))
	var m2 float64 // total degree (2m)
	for k, wt := range w {
		deg[k[0]] += wt
		deg[k[1]] += wt
		m2 += 2 * wt
	}

	comm := make([]int, len(nodes)) // node index -> community label
	for i := range comm {
		comm[i] = i
	}
	if m2 > 0 {
		mergeCommunities(comm, w, deg, m2)
	}

	groups := map[int][]string{}
	for i, c := range comm {
		groups[c] = append(groups[c], nodes[i])
	}
	out := make([][]string, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
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

// mergeCommunities greedily merges the connected community pair with the
// highest positive modularity gain until no merge improves modularity.
// comm is updated in place. Gain for communities A, B (CNM):
// dQ = 2 * ( w(A,B)/2m - (deg(A)/2m)*(deg(B)/2m) ).
func mergeCommunities(comm []int, w map[[2]int]float64, deg []float64, m2 float64) {
	cw := make(map[[2]int]float64, len(w)) // inter-community weight, key (low, high)
	for k, wt := range w {
		cw[k] = wt
	}
	cdeg := append([]float64(nil), deg...)

	for {
		bestGain := 0.0
		best := [2]int{-1, -1}
		pairs := make([][2]int, 0, len(cw))
		for k := range cw {
			pairs = append(pairs, k)
		}
		// Stable scan order makes tie-breaking (and thus output) deterministic.
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i][0] != pairs[j][0] {
				return pairs[i][0] < pairs[j][0]
			}
			return pairs[i][1] < pairs[j][1]
		})
		for _, k := range pairs {
			gain := 2 * (cw[k]/m2 - (cdeg[k[0]]/m2)*(cdeg[k[1]]/m2))
			if gain > bestGain+1e-12 {
				bestGain, best = gain, k
			}
		}
		if best[0] < 0 {
			return
		}
		a, b := best[0], best[1] // merge b into a (a < b keeps labels stable)
		for i := range comm {
			if comm[i] == b {
				comm[i] = a
			}
		}
		next := make(map[[2]int]float64, len(cw))
		for k, wt := range cw {
			x, y := k[0], k[1]
			if x == b {
				x = a
			}
			if y == b {
				y = a
			}
			if x == y {
				continue // became internal weight; not needed for the gain formula
			}
			if x > y {
				x, y = y, x
			}
			next[[2]int{x, y}] += wt
		}
		cw = next
		cdeg[a] += cdeg[b]
		cdeg[b] = 0
	}
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
