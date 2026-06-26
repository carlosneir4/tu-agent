package codegen

// SourceUnit is the language-neutral input to domain mapping: only the facts
// clustering needs, independent of how they were parsed. Plan 2 populates it
// from Java (via the graph store); Plan 3 adds other languages without touching
// the clustering algorithm.
type SourceUnit struct {
	Path     string // repo-relative
	Package  string // grouping key: dotted package for Java
	FQN      string // fully-qualified type name, for structural context
	Size     int    // source bytes; drives byte-based domain splitting
	Language string
}
