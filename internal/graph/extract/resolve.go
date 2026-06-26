package extract

import (
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

const maxAmbiguous = 5

type symbolTable struct {
	byFQN      map[string]string
	bySimple   map[string][]string
	byPkg      map[string][]string
	pkgOf      map[string]string
	fileOf     map[string]string
	methodsOf  map[string][]string
	methByName map[string][]string
	testByName map[string][]string
}

func buildSymbols(nodes []graph.Node, metas []graph.FileMeta) *symbolTable {
	pkgByPath := map[string]string{}
	for _, m := range metas {
		pkgByPath[m.Path] = m.Package
	}
	st := &symbolTable{
		byFQN: map[string]string{}, bySimple: map[string][]string{},
		byPkg: map[string][]string{}, pkgOf: map[string]string{},
		fileOf: map[string]string{}, methodsOf: map[string][]string{},
		methByName: map[string][]string{}, testByName: map[string][]string{},
	}
	for _, n := range nodes {
		st.fileOf[n.ID] = n.Path
		switch n.Kind {
		case graph.KindClass, graph.KindTest:
			pkg := pkgByPath[n.Path]
			st.pkgOf[n.ID] = pkg
			simple := n.Name
			st.bySimple[simple] = append(st.bySimple[simple], n.ID)
			st.byPkg[pkg] = append(st.byPkg[pkg], n.ID)
			fqn := simple
			if pkg != "" {
				fqn = pkg + "." + simple
			}
			st.byFQN[fqn] = n.ID
			if n.Kind == graph.KindTest {
				st.testByName[simple] = append(st.testByName[simple], n.ID)
			}
		case graph.KindFunction:
			if dot := strings.LastIndex(n.Name, "."); dot > 0 {
				owner := n.Path + "::" + n.Name[:dot]
				st.methodsOf[owner] = append(st.methodsOf[owner], n.ID)
				st.methByName[n.Name[dot+1:]] = append(st.methByName[n.Name[dot+1:]], n.ID)
			} else {
				// Free function (Go/Python): callable by its bare name.
				st.methByName[n.Name] = append(st.methByName[n.Name], n.ID)
			}
		}
	}
	return st
}

// ResolveWithNodes turns refs and import lists into typed edges plus the
// external stub nodes it synthesized. modulePath is the Go module path (from
// go.mod), used to map in-module Go package imports to their files; pass "" for
// non-Go projects.
func ResolveWithNodes(nodes []graph.Node, metas []graph.FileMeta, refs []graph.Ref, modulePath string) ([]graph.Edge, []graph.Node) {
	st := buildSymbols(nodes, metas)
	metaByPath := map[string]graph.FileMeta{}
	for _, m := range metas {
		metaByPath[m.Path] = m
	}

	seen := map[graph.Edge]bool{}
	var edges []graph.Edge
	var stubNodes []graph.Node
	add := func(e graph.Edge) {
		if e.From == "" || e.To == "" || e.From == e.To || seen[e] {
			return
		}
		seen[e] = true
		edges = append(edges, e)
	}

	// filesByPkg maps a package (repo-relative path, as set by the extractor) to
	// the files that compose it. Go imports resolve at file granularity through
	// this map, so a package of pure functions (no type nodes) is still linked.
	filesByPkg := map[string][]string{}
	for _, m := range metas {
		filesByPkg[m.Package] = append(filesByPkg[m.Package], m.Path)
	}

	// 1. File-level imports edges.
	for _, m := range metas {
		for _, imp := range m.Imports {
			// Go: imports are package paths. An in-module import (prefixed by the
			// module path) maps to a repo-relative package; everything else
			// (stdlib, third-party) is external and produces no edge.
			if m.Language == "go" {
				if modulePath == "" {
					continue
				}
				rel, ok := strings.CutPrefix(imp, modulePath+"/")
				if !ok {
					if imp != modulePath {
						continue
					}
					rel = "" // import of the module root package
				}
				for _, p := range filesByPkg[rel] {
					add(graph.Edge{From: m.Path, To: p, Kind: graph.EdgeImports, Confidence: graph.ConfExact})
				}
				continue
			}
			if pkg, ok := strings.CutSuffix(imp, ".*"); ok {
				for _, id := range st.byPkg[pkg] {
					add(graph.Edge{From: m.Path, To: st.fileOf[id], Kind: graph.EdgeImports, Confidence: graph.ConfExact})
				}
				continue
			}
			if id, ok := st.byFQN[imp]; ok {
				add(graph.Edge{From: m.Path, To: st.fileOf[id], Kind: graph.EdgeImports, Confidence: graph.ConfExact})
			}
		}
	}

	// 2a. Resolve inheritance refs first so parentsOf is available to calls.
	for _, r := range refs {
		if r.Kind != graph.EdgeExtends && r.Kind != graph.EdgeImplements {
			continue
		}
		fromPath := pathOfID(r.FromID)
		meta := metaByPath[fromPath]
		for _, c := range resolveClass(st, meta, fromPath, r.Name) {
			add(graph.Edge{From: r.FromID, To: c.id, Kind: r.Kind, Confidence: c.conf})
		}
	}

	// 2b. Build parentsOf from the resolved inheritance edges.
	parentsOf := map[string][]string{}
	for _, e := range edges {
		if e.Kind == graph.EdgeExtends || e.Kind == graph.EdgeImplements {
			parentsOf[e.From] = append(parentsOf[e.From], e.To)
		}
	}

	// 2c. Resolve calls, with inheritance-aware fallback.
	for _, r := range refs {
		if r.Kind != graph.EdgeCalls {
			continue
		}
		if isLoggerCall(r.Recv, r.Name) {
			continue
		}
		fromPath := pathOfID(r.FromID)
		meta := metaByPath[fromPath]
		for _, c := range resolveCall(st, meta, fromPath, r.Name, parentsOf) {
			add(graph.Edge{From: r.FromID, To: c.id, Kind: graph.EdgeCalls, Confidence: c.conf})
		}
	}

	// 2d. Resolve GraphQL fragment spreads by globally-unique fragment name.
	fragmentByName := map[string]string{}
	for _, n := range nodes {
		if n.Kind == graph.KindGraphQLFragment {
			fragmentByName[n.Name] = n.ID
		}
	}
	for _, r := range refs {
		if r.Kind != graph.EdgeSpreads {
			continue
		}
		if id, ok := fragmentByName[r.Name]; ok {
			add(graph.Edge{From: r.FromID, To: id, Kind: graph.EdgeSpreads, Confidence: graph.ConfExact})
		}
	}

	// 2e. Resolve fragment on-types to schema type nodes (by unique type name).
	typesByName := map[string]string{}
	for _, n := range nodes {
		if n.Kind == graph.KindGraphQLType {
			typesByName[n.Name] = n.ID
		}
	}
	for _, n := range nodes {
		if n.Kind != graph.KindGraphQLFragment {
			continue
		}
		if id, ok := typesByName[n.ReturnType]; ok {
			add(graph.Edge{From: n.ID, To: id, Kind: graph.EdgeOnType, Confidence: graph.ConfExact})
		}
	}

	// 3. tested_by from naming convention.
	for _, n := range nodes {
		if n.Kind != graph.KindClass {
			continue
		}
		for _, cand := range []string{n.Name + "Test", n.Name + "Tests", n.Name + "IT", "Test" + n.Name} {
			for _, tid := range st.testByName[cand] {
				add(graph.Edge{From: n.ID, To: tid, Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh})
			}
		}
	}

	// 3b. TypeScript sibling-test convention: foo.test.ts(x) / foo.spec.ts(x)
	// (also under __tests__/) tests foo.ts(x). File-level edge at ConfHigh.
	fileExists := map[string]bool{}
	for _, m := range metas {
		fileExists[m.Path] = true
	}
	for _, m := range metas {
		if m.Language != "typescript" {
			continue
		}
		for _, srcPath := range tsTestSources(m.Path) {
			if fileExists[srcPath] && srcPath != m.Path {
				add(graph.Edge{From: srcPath, To: m.Path, Kind: graph.EdgeTestedBy, Confidence: graph.ConfHigh})
			}
		}
	}

	// 4. Overrides: for each method of a child class, if a resolved direct parent
	// has a method with the same bare name, link child method -> parent method.
	// parentsOf was built in section 2b.
	for classID, parentIDs := range parentsOf {
		for _, childMethID := range st.methodsOf[classID] {
			childName := childMethID[strings.LastIndex(childMethID, ".")+1:]
			for _, parentID := range parentIDs {
				for _, parentMethID := range st.methodsOf[parentID] {
					parentName := parentMethID[strings.LastIndex(parentMethID, ".")+1:]
					if childName == parentName {
						add(graph.Edge{
							From:       childMethID,
							To:         parentMethID,
							Kind:       graph.EdgeOverrides,
							Confidence: graph.ConfHigh,
						})
					}
				}
			}
		}
	}

	// Synthesize external stub nodes for edge targets named "external::<fqn>".
	known := map[string]bool{}
	for _, n := range nodes {
		known[n.ID] = true
	}
	for _, e := range edges {
		if !strings.HasPrefix(e.To, "external::") || known[e.To] {
			continue
		}
		known[e.To] = true
		stubNodes = append(stubNodes, graph.Node{
			ID:   e.To,
			Kind: graph.KindExternal,
			Name: strings.TrimPrefix(e.To, "external::"),
		})
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return string(edges[i].Kind) < string(edges[j].Kind)
	})
	return edges, stubNodes
}

type candidate struct {
	id   string
	conf graph.Confidence
}

func resolveClass(st *symbolTable, meta graph.FileMeta, fromPath, name string) []candidate {
	if strings.Contains(name, ".") {
		if id, ok := st.byFQN[name]; ok {
			return []candidate{{id, graph.ConfExact}}
		}
		return nil
	}
	for _, id := range st.bySimple[name] {
		if st.fileOf[id] == fromPath {
			return []candidate{{id, graph.ConfExact}}
		}
	}
	if meta.Package != "" {
		if id, ok := st.byFQN[meta.Package+"."+name]; ok {
			return []candidate{{id, graph.ConfExact}}
		}
	}
	var wild []string
	for _, imp := range meta.Imports {
		if strings.HasSuffix(imp, "."+name) {
			if id, ok := st.byFQN[imp]; ok {
				return []candidate{{id, graph.ConfExact}}
			}
			// Named explicitly in imports but not in symbol table: it's external.
			// Emit a stub candidate so the inheritance/import fact survives.
			return []candidate{{"external::" + imp, graph.ConfExact}}
		}
		if pkg, ok := strings.CutSuffix(imp, ".*"); ok {
			if id, ok := st.byFQN[pkg+"."+name]; ok {
				wild = append(wild, id)
			}
		}
	}
	if len(wild) == 1 {
		return []candidate{{wild[0], graph.ConfHigh}}
	}
	if len(wild) > 1 {
		return lowAll(wild)
	}
	global := st.bySimple[name]
	if len(global) == 1 {
		return []candidate{{global[0], graph.ConfHigh}}
	}
	if len(global) >= 2 && len(global) <= maxAmbiguous {
		return lowAll(global)
	}
	return nil
}

// isLoggerCall reports whether a call is a logging-level method invoked on a
// conventionally-named logger receiver (LOGGER.error, log.warn, …). Resolution
// is by method name only — it has no receiver types — so without this guard a
// LOGGER.error() call binds to any visible project method named error(). This
// suppresses the canonical false positive; full receiver-type resolution is
// Phase-3 (LSP) work.
func isLoggerCall(recv, name string) bool {
	switch strings.ToLower(recv) {
	case "log", "logger":
	default:
		return false
	}
	switch name {
	case "error", "warn", "warning", "info", "debug", "trace", "fatal":
		return true
	}
	return false
}

func resolveCall(st *symbolTable, meta graph.FileMeta, fromPath, name string, parentsOf map[string][]string) []candidate {
	all := st.methByName[name]
	if len(all) == 0 {
		return nil
	}

	// Inheritance-first: if the caller's class inherits a method with this name
	// from a resolved parent, bind to the parent method at ConfMedium. This
	// takes priority over the general visibility check because the call is
	// dispatched through the inheritance chain, not a direct import.
	for classID, parentIDs := range parentsOf {
		if st.fileOf[classID] != fromPath {
			continue
		}
		for _, parentID := range parentIDs {
			for _, methID := range st.methodsOf[parentID] {
				methName := methID[strings.LastIndex(methID, ".")+1:]
				if methName == name {
					return []candidate{{methID, graph.ConfMedium}}
				}
			}
		}
	}

	visible := func(methodID string) bool {
		mPath := st.fileOf[methodID]
		if mPath == fromPath {
			return true
		}
		for _, id := range st.byPkg[meta.Package] {
			if st.fileOf[id] == mPath {
				return true
			}
		}
		for _, imp := range meta.Imports {
			if pkg, ok := strings.CutSuffix(imp, ".*"); ok {
				for _, id := range st.byPkg[pkg] {
					if st.fileOf[id] == mPath {
						return true
					}
				}
			} else if id, ok := st.byFQN[imp]; ok && st.fileOf[id] == mPath {
				return true
			}
		}
		return false
	}
	var vis []string
	for _, id := range all {
		if visible(id) {
			vis = append(vis, id)
		}
	}
	switch {
	case len(vis) == 1:
		return []candidate{{vis[0], graph.ConfHigh}}
	case len(vis) >= 2 && len(vis) <= maxAmbiguous:
		return lowAll(vis)
	case len(vis) == 0 && len(all) == 1:
		return []candidate{{all[0], graph.ConfMedium}}
	case len(vis) == 0 && len(all) <= maxAmbiguous:
		return lowAll(all)
	}
	return nil
}

func lowAll(ids []string) []candidate {
	out := make([]candidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, candidate{id, graph.ConfLow})
	}
	return out
}

func pathOfID(id string) string {
	path, _, _ := strings.Cut(id, "::")
	return path
}
