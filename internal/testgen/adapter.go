// Package testgen generates unit tests for graph-resolved symbols and
// verifies them with a bounded compile/run/repair loop.
package testgen

import (
	"fmt"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// Target is the resolved symbol a test will be generated for.
type Target struct {
	NodeID     string `json:"node_id"`
	Name       string `json:"name"` // simple name; methods as "Class.method"
	Path       string `json:"path"` // repo-relative source file
	Line       int    `json:"line"`
	EndLine    int    `json:"end_line"`
	Language   string `json:"language"` // "go" | "java" | "python" | "typescript"
	Params     string `json:"params"`
	ReturnType string `json:"return_type"`
}

// TargetFromNode builds a Target from a resolved graph node.
func TargetFromNode(n graph.Node) Target {
	return Target{
		NodeID: n.ID, Name: n.Name, Path: n.Path, Line: n.Line, EndLine: n.EndLine,
		Language: n.Language, Params: n.Params, ReturnType: n.ReturnType,
	}
}

// TestFuncPrefix returns the mandatory name prefix for generated test
// functions: "Test" + symbol name with dots stripped ("Store.Save" →
// "TestStoreSave"). Scoped Go runs match this prefix, so it must be
// derivable before generation (spec: test_scaffold requirement).
func (t Target) TestFuncPrefix() string {
	return "Test" + strings.ReplaceAll(t.Name, ".", "")
}

// LanguageAdapter owns the language-specific facts of test generation.
// The pipeline owns everything else: context, generation, repair (spec
// decision 4 — thin adapter).
type LanguageAdapter interface {
	// Detect returns nil when the repo has a usable test runner.
	Detect(repoRoot string) error
	// TestPath returns the conventional test file path. Generated tests are
	// merged into this file (marked by name/sentinels), never a *Gen* sibling.
	TestPath(repoRoot string, t Target) (string, error)
	// PromptFragment returns language/framework conventions for the prompt.
	PromptFragment(t Target, testPath string) string
	// RunCommand returns the argv that compiles and runs ONLY this target's
	// generated tests — never hand-written tests or the full suite.
	RunCommand(repoRoot, testPath string, t Target) ([]string, error)
}

// AdapterFor returns the adapter for a graph language name.
func AdapterFor(language string) (LanguageAdapter, error) {
	switch language {
	case "go":
		return &GoAdapter{}, nil
	case "java":
		return &JavaAdapter{}, nil
	case "python":
		return &PythonAdapter{}, nil
	case "typescript":
		return &TSAdapter{}, nil
	default:
		return nil, fmt.Errorf("testgen.AdapterFor: no adapter for language %q (supported: go, java, python, typescript)", language)
	}
}
