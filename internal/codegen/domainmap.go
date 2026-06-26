package codegen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Domain is a package-cluster of source files that becomes one SKILL.md.
// When Parent != "" this is a sub-domain rendered under the parent skill's
// references/ directory. Parent markers (Files == nil, Parent == "") hold
// the parent's name and package for index rendering but generate no LLM call.
type Domain struct {
	Name    string   // kebab-case identifier derived from the package path
	Package string   // dotted package prefix that defines the domain
	Files   []string // repo-relative paths owned by this domain; nil on parent markers
	Parent  string   // non-empty: this sub-domain belongs under the named parent skill
}

// DomainMapOptions tunes how files are clustered into domains.
type DomainMapOptions struct {
	Depth              int // package segments below the common root used for the initial split
	MinFiles           int // domains smaller than this are merged into a sibling
	MaxFiles           int // domains larger than this are split by sub-package
	MaxBytes           int // optional: split leaf domains whose source bytes exceed this; 0 = off
	MinStandaloneFiles int // domains with >= this many non-test files are never merged; 0 = off
}

// BuildDomainMap groups files into domains by package path (Depth), then
// refines using dependency edges (merge tiny, split huge). It is pure and
// deterministic: identical inputs always yield identical output.
func BuildDomainMap(files []SourceUnit, edges []Edge, opts DomainMapOptions) []Domain {
	if opts.Depth < 1 {
		opts.Depth = 1
	}
	root := commonPackagePrefix(files)
	groups := map[string][]string{} // domain package -> files
	for _, f := range files {
		dp := domainPackage(f.Package, root, opts.Depth)
		groups[dp] = append(groups[dp], f.Path)
	}
	domains := make([]Domain, 0, len(groups))
	for pkg, fs := range groups {
		sort.Strings(fs)
		domains = append(domains, Domain{Name: kebabPackage(pkg, root), Package: pkg, Files: fs})
	}
	sortDomains(domains)
	domains = mergeTinyDomains(domains, edges, opts.MinFiles, opts.MinStandaloneFiles)
	domains = splitHugeDomains(domains, byPath(files), opts.MaxFiles, opts.MaxBytes, opts.Depth)
	sortDomains(domains)
	return domains
}

// byPath indexes parsed files by their repo-relative path.
func byPath(files []SourceUnit) map[string]SourceUnit {
	m := make(map[string]SourceUnit, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

// splitHugeDomains divides any domain above maxFiles or maxBytes along its next
// package segment (depth+1). A subgroup that is still too large is split again
// one level deeper; when no finer package boundary exists the domain is batched
// by bytes (if maxBytes > 0) or left intact.
func splitHugeDomains(domains []Domain, files map[string]SourceUnit, maxFiles, maxBytes, depth int) []Domain {
	if maxFiles <= 0 && maxBytes <= 0 {
		return domains
	}
	var out []Domain
	for _, d := range domains {
		out = append(out, splitOne(d, files, maxFiles, maxBytes, depth)...)
	}
	return out
}

// domainBytes returns the total source bytes for all files in a domain.
func domainBytes(d Domain, files map[string]SourceUnit) int {
	var sum int
	for _, p := range d.Files {
		sum += files[p].Size
	}
	return sum
}

// splitOne splits d if it exceeds the size limits. When a real split occurs the
// original domain is kept as a parent marker (Files==nil) and every leaf child
// carries Parent set to the top-level ancestor (exactly two levels deep: a child
// of an already-split domain keeps its grandparent as Parent, never grandparent's
// parent). If d is already within limits it is returned as-is.
func splitOne(d Domain, files map[string]SourceUnit, maxFiles, maxBytes, depth int) []Domain {
	parts := splitParts(d, files, maxFiles, maxBytes, depth)
	if len(parts) == 1 {
		return parts
	}
	// A real split happened: emit the parent marker + tag each leaf with the
	// top-level ancestor (cap hierarchy at two levels).
	top := d.Name
	if d.Parent != "" {
		top = d.Parent
	}
	out := make([]Domain, 0, len(parts)+1)
	if d.Parent == "" {
		out = append(out, Domain{Name: d.Name, Package: d.Package, Files: nil})
	}
	for _, p := range parts {
		p.Parent = top
		out = append(out, p)
	}
	sortDomains(out)
	return out
}

// splitParts returns the leaf pieces of d without emitting parent markers.
// It contains the original split logic extracted from splitOne.
func splitParts(d Domain, files map[string]SourceUnit, maxFiles, maxBytes, depth int) []Domain {
	overCount := maxFiles > 0 && len(d.Files) > maxFiles
	overBytes := maxBytes > 0 && domainBytes(d, files) > maxBytes
	if !overCount && !overBytes {
		return []Domain{d}
	}
	// Prefer sub-package split first.
	groups := map[string][]string{}
	for _, p := range d.Files {
		seg := nextSegment(files[p].Package, d.Package)
		groups[seg] = append(groups[seg], p)
	}
	if len(groups) >= 2 {
		var res []Domain
		for seg, fs := range groups {
			sort.Strings(fs)
			name := d.Name
			pkg := d.Package
			if seg != "" {
				name = d.Name + "-" + strings.ReplaceAll(strings.ToLower(seg), ".", "-")
				pkg = d.Package + "." + seg
			}
			res = append(res, splitParts(Domain{Name: name, Package: pkg, Files: fs}, files, maxFiles, maxBytes, depth+1)...)
		}
		sortDomains(res)
		return res
	}
	// Leaf package: only byte overflow forces batching.
	if overBytes {
		return batchByBytes(d, files, maxBytes)
	}
	return []Domain{d}
}

// batchByBytes splits a leaf domain into sequential batches where each batch's
// total byte size does not exceed maxBytes. Files are sorted for determinism.
// An oversized single file gets its own batch regardless of the limit.
func batchByBytes(d Domain, files map[string]SourceUnit, maxBytes int) []Domain {
	paths := append([]string(nil), d.Files...)
	sort.Strings(paths)
	var batches [][]string
	var cur []string
	var curBytes int
	for _, p := range paths {
		size := files[p].Size
		if len(cur) > 0 && curBytes+size > maxBytes {
			batches = append(batches, cur)
			cur, curBytes = nil, 0
		}
		cur = append(cur, p)
		curBytes += size
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	if len(batches) == 1 {
		return []Domain{d}
	}
	out := make([]Domain, 0, len(batches))
	for i, fs := range batches {
		out = append(out, Domain{
			Name:    fmt.Sprintf("%s-%d", d.Name, i+1),
			Package: d.Package,
			Files:   fs,
		})
	}
	return out
}

// nextSegment returns the package segment immediately after parentPkg in pkg,
// or "" when pkg has no deeper segment.
func nextSegment(pkg, parentPkg string) string {
	rest := strings.TrimPrefix(pkg, parentPkg)
	rest = strings.TrimPrefix(rest, ".")
	if rest == "" {
		return ""
	}
	return strings.Split(rest, ".")[0]
}

// normalizePkg converts both slash-separated and dot-separated package strings
// to a canonical dot-separated form for subtree comparisons.
func normalizePkg(pkg string) string {
	return strings.ReplaceAll(strings.ReplaceAll(pkg, "/", "."), "-", ".")
}

// parentPackage returns the normalized parent of a package (everything before
// the last dot after normalization). Returns the package itself when there is
// no dot (i.e. the package is already at the top level).
func parentPackage(pkg string) string {
	norm := normalizePkg(pkg)
	if i := strings.LastIndexByte(norm, '.'); i >= 0 {
		return norm[:i]
	}
	return norm
}

// sameSubtree reports whether candPkg lives inside the same parent-package
// subtree as srcPkg. A top-level package (no dot after normalization) is
// considered to share a subtree with everything.
func sameSubtree(srcPkg, candPkg string) bool {
	parent := parentPackage(srcPkg)
	if parent == "" {
		return true
	}
	cand := normalizePkg(candPkg)
	return cand == parent || strings.HasPrefix(cand, parent+".")
}

// mergeTinyDomains folds every domain below minFiles into the surviving domain
// it shares the most dependency edges with, provided the target is in the same
// parent-package subtree. Domains with >= minStandalone non-test files are
// never merged regardless of total file count. When no qualifying target exists
// the tiny domain is left as-is (no cross-tree fallback).
func mergeTinyDomains(domains []Domain, edges []Edge, minFiles, minStandalone int) []Domain {
	if minFiles <= 1 || len(domains) < 2 {
		return domains
	}
	fileToDomain := map[string]string{}
	for _, d := range domains {
		for _, f := range d.Files {
			fileToDomain[f] = d.Name
		}
	}
	// coupling[a][b] = number of edges between domains a and b (either direction).
	coupling := map[string]map[string]int{}
	bump := func(a, b string) {
		if a == "" || b == "" || a == b {
			return
		}
		if coupling[a] == nil {
			coupling[a] = map[string]int{}
		}
		coupling[a][b]++
	}
	for _, e := range edges {
		fd, td := fileToDomain[e.From], fileToDomain[e.To]
		bump(fd, td)
		bump(td, fd)
	}

	byName := map[string]*Domain{}
	for i := range domains {
		byName[domains[i].Name] = &domains[i]
	}
	merged := map[string]bool{}
	// Process tiny domains in a stable order.
	order := append([]Domain(nil), domains...)
	sortDomains(order)
	for _, d := range order {
		if merged[d.Name] || len(d.Files) >= minFiles {
			continue
		}
		// A domain with enough non-test files stands alone even if total < minFiles.
		if minStandalone > 0 && nonTestFileCount(d) >= minStandalone {
			continue
		}
		target := pickMergeTarget(d, byName, merged, coupling)
		if target == "" {
			// Nothing to merge into; leave as-is. We deliberately do NOT drop
			// "uncoupled" tiny domains here: this function only sees import edges,
			// so call/extends-related domains (e.g. same-package Java) look
			// uncoupled and would be wrongly dropped, emptying the index on
			// import-light repos. Dropping genuinely-disconnected (zero-degree)
			// noise must use full-degree info, which lives in the clustered path.
			continue
		}
		dst := byName[target]
		dst.Files = append(dst.Files, byName[d.Name].Files...)
		sort.Strings(dst.Files)
		merged[d.Name] = true
	}
	out := make([]Domain, 0, len(domains))
	for i := range domains {
		if !merged[domains[i].Name] {
			out = append(out, domains[i])
		}
	}
	return out
}

// pickMergeTarget returns the name of the best surviving domain to absorb src.
// A candidate must (a) have at least one coupling edge with src and (b) live in
// the same parent-package subtree as src. Among qualifying candidates the most
// coupled wins; ties break by file count then name. Returns "" when no
// qualifying candidate exists (the caller leaves the tiny domain as-is).
func pickMergeTarget(src Domain, byName map[string]*Domain, merged map[string]bool, coupling map[string]map[string]int) string {
	type cand struct {
		name  string
		edges int
		files int
	}
	var cands []cand
	for name, d := range byName {
		if name == src.Name || merged[name] {
			continue
		}
		edgeCount := coupling[src.Name][name]
		if edgeCount == 0 {
			continue // require at least one coupling edge
		}
		if !sameSubtree(src.Package, d.Package) {
			continue // block cross-tree merges
		}
		cands = append(cands, cand{name: name, edges: edgeCount, files: len(d.Files)})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].edges != cands[j].edges {
			return cands[i].edges > cands[j].edges
		}
		if cands[i].files != cands[j].files {
			return cands[i].files > cands[j].files
		}
		return cands[i].name < cands[j].name
	})
	return cands[0].name
}

// commonPackagePrefix returns the longest dot-delimited package prefix shared
// by every file that declares a package. Files without a package are ignored.
func commonPackagePrefix(files []SourceUnit) string {
	var prefix []string
	first := true
	for _, f := range files {
		if f.Package == "" {
			continue
		}
		segs := strings.Split(f.Package, ".")
		if first {
			prefix = segs
			first = false
			continue
		}
		n := len(prefix)
		if len(segs) < n {
			n = len(segs)
		}
		i := 0
		for ; i < n; i++ {
			if prefix[i] != segs[i] {
				break
			}
		}
		prefix = prefix[:i]
	}
	return strings.Join(prefix, ".")
}

// domainPackage returns the package prefix identifying a file's domain: the
// common root plus Depth additional segments. Files whose package equals the
// root (or is empty) map to the root itself.
func domainPackage(pkg, root string, depth int) string {
	if pkg == "" {
		return root
	}
	rest := strings.TrimPrefix(pkg, root)
	rest = strings.TrimPrefix(rest, ".")
	if rest == "" {
		return root
	}
	segs := strings.Split(rest, ".")
	if len(segs) > depth {
		segs = segs[:depth]
	}
	if root == "" {
		return strings.Join(segs, ".")
	}
	return root + "." + strings.Join(segs, ".")
}

// kebabPackage renders the domain-distinguishing segments (package minus root)
// as a kebab-case name. The root itself becomes "core". Packages may be
// dot-separated (Java) or slash-separated (Go); both separators collapse to "-"
// so the name is a single flat path segment (filepath.Join must not nest it).
func kebabPackage(pkg, root string) string {
	rest := strings.TrimPrefix(pkg, root)
	rest = strings.TrimLeft(rest, "./")
	if rest == "" {
		return "core"
	}
	rest = strings.ToLower(rest)
	rest = strings.ReplaceAll(rest, ".", "-")
	rest = strings.ReplaceAll(rest, "/", "-")
	return rest
}

func sortDomains(ds []Domain) {
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })
}

// IsTestFile reports whether a path names a test file by convention.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.py") ||
		strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java") {
		return true
	}
	return strings.HasPrefix(base, "test_")
}

// nonTestFileCount counts a domain's non-test source files.
func nonTestFileCount(d Domain) int {
	n := 0
	for _, p := range d.Files {
		if !IsTestFile(p) {
			n++
		}
	}
	return n
}
