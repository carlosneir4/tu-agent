// Package graph defines the shared model for the code knowledge graph.
package graph

// NodeKind classifies graph nodes.
type NodeKind string

const (
	KindFile             NodeKind = "file"
	KindClass            NodeKind = "class" // includes interfaces and enums
	KindFunction         NodeKind = "function"
	KindTest             NodeKind = "test"
	KindSkill            NodeKind = "skill"      // a generated SKILL.md
	KindConvention       NodeKind = "convention" // a CLAUDE.md conventions block
	KindExternal         NodeKind = "external"   // symbol from a compiled dependency (no source path)
	KindGraphQLFragment  NodeKind = "graphql_fragment"
	KindGraphQLOperation NodeKind = "graphql_operation"
	KindGraphQLType      NodeKind = "graphql_type"
)

// EdgeKind classifies graph edges. From depends on To.
type EdgeKind string

const (
	EdgeContains   EdgeKind = "contains"
	EdgeImports    EdgeKind = "imports"
	EdgeExtends    EdgeKind = "extends"
	EdgeImplements EdgeKind = "implements"
	EdgeCalls      EdgeKind = "calls"
	EdgeTestedBy   EdgeKind = "tested_by"
	EdgeDocuments  EdgeKind = "documents" // skill/convention -> code it describes
	EdgeOverrides  EdgeKind = "overrides" // child method overrides parent method; From=child method, To=parent method
	EdgeSpreads    EdgeKind = "spreads"   // a GraphQL operation/fragment composes a fragment
	EdgeOnType     EdgeKind = "on_type"   // a GraphQL fragment is defined on a type
)

// Confidence records how a heuristic edge was resolved.
type Confidence string

const (
	ConfExact  Confidence = "exact"
	ConfHigh   Confidence = "high"
	ConfMedium Confidence = "medium"
	ConfLow    Confidence = "low"
)

// Node is a code entity. ID format: "<path>" for files,
// "<path>::<Class>" for classes, "<path>::<Class>.<method>" for functions,
// "skill::<name>" for skill nodes, "convention::<name>" for convention nodes.
type Node struct {
	ID         string
	Kind       NodeKind
	Name       string // simple name; methods as "Class.method"
	Path       string // repo-relative
	Line       int    // 1-based
	EndLine    int
	Language   string
	Params     string
	ReturnType string
	Exported   bool // public/exported visibility; populated for function nodes
}

// Edge is a directed dependency: From depends on To.
type Edge struct {
	From       string
	To         string
	Kind       EdgeKind
	Confidence Confidence
}

// Ref is an unresolved reference recorded at parse time and resolved
// project-wide afterwards. Persisted so update never re-parses unchanged files.
type Ref struct {
	FromID string   // node where the reference occurs
	Kind   EdgeKind // extends | implements | calls
	Name   string   // simple or qualified name as written
	Recv   string   // call receiver as written ("LOGGER", "this", ""); calls only
	Line   int
}

// FileMeta is per-file resolution context persisted alongside nodes.
type FileMeta struct {
	Path     string
	SHA256   string
	Language string
	Status   string   // "ok" | "failed"
	Package  string   // e.g. "com.acme.billing"; "" if none
	Imports  []string // explicit imports; wildcards kept as "a.b.*"
}
