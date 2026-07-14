package codegen

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/orchestrator"
	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

// DomainFact is the compact per-domain input to architecture synthesis.
type DomainFact struct {
	Name        string
	Description string
	KeyFiles    []string
}

// BuildSynthesisPrompt is the system prompt for architecture-overview synthesis.
func BuildSynthesisPrompt() string {
	return `You are a software architect writing ONE architecture overview as a SKILL.md.

Write exactly this structure:
---
name: architecture
description: Project architecture overview — domains, navigation, and change-impact map.
---
# Architecture Overview

## Purpose
<1 paragraph: what the project does, end to end.>

## Domains
| Domain | What it does | Key files |
|--------|--------------|-----------|
<one row per domain.>

## How Domains Connect
<domain-level flow phrased from the dependency edges provided.>

## Blast Radius
<If a "Cyclic core" is provided, report it ONCE: "Cyclic core (co-coupled — a
change anywhere ripples across all): <members>". Do NOT list per-domain
dependents for core members. For domains OUTSIDE the core, write "Changing
<domain> affects: <dependents>".>

## Change-Impact Queries
For file- or symbol-level precision beyond the domain-level Blast Radius above,
query the graph (authoritative for structure — callers, dependents, tests):
- If you have the graph tools:  get_context(<file-or-symbol>) (also get_impact, find_symbol)
- Otherwise, via CLI:           tu-agent graph context <file-or-symbol>
                                tu-agent graph impact  <symbol>
get_context returns blast radius (dependents), the relevant concept card(s),
conventions, and tests to run — pointers, not source.

Rules:
- Use ONLY the domains and dependency edges provided. Invent nothing.
- Dependency edges are facts. Blast Radius is the reverse direction: who depends on a domain.
- Treat any provided "Cyclic core" as authoritative; never expand it into per-member dependent lists.
- Include the Change-Impact Queries section verbatim; it is static and does not depend on the project.
- If no dependency edges are provided, write "Dependency data unavailable" under both
  How Domains Connect and Blast Radius.
- Output ONLY the SKILL.md content starting with '---'. Do not call any tools.`
}

const (
	maxKeyFilesPerDomain  = 3    // keep only the shortest paths (most likely top-level)
	synthesisReservedOut  = 2048 // tokens reserved for model output
	synthesisSystemTokens = 500  // approximate system prompt size
)

// largestCyclicCore returns the biggest strongly-connected component (>1 member)
// among the domain edges, or nil when the domain graph is acyclic.
func largestCyclicCore(domainEdges []Edge) []string {
	adj := make(map[string][]string, len(domainEdges))
	for _, e := range domainEdges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	for _, comp := range graph.StronglyConnectedComponents(adj) {
		if len(comp) > 1 {
			return comp // components are size-desc sorted, so the first >1 is largest
		}
	}
	return nil
}

// BuildSynthesisMessage builds the compact user message within a byte budget
// so it fits in small context windows. Key files are capped per domain;
// domains are dropped (least-connected first) if the message still overflows.
// maxBytes = 0 means no limit.
func BuildSynthesisMessage(project string, domains []DomainFact, domainEdges []Edge, maxBytes int) string {
	ds := make([]DomainFact, len(domains))
	copy(ds, domains)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })

	// Trim key files to the shortest maxKeyFilesPerDomain paths.
	for i := range ds {
		if len(ds[i].KeyFiles) > maxKeyFilesPerDomain {
			kf := make([]string, len(ds[i].KeyFiles))
			copy(kf, ds[i].KeyFiles)
			sort.Slice(kf, func(a, b int) bool { return len(kf[a]) < len(kf[b]) })
			ds[i].KeyFiles = kf[:maxKeyFilesPerDomain]
		}
	}

	build := func(subset []DomainFact) string {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Project: %s\n\nDomains:\n", project)
		for _, d := range subset {
			fmt.Fprintf(&sb, "\n- %s: %s\n  Key files: %s\n", d.Name, d.Description, strings.Join(d.KeyFiles, ", "))
		}
		sb.WriteString("\nDependencies (domain -> domain):\n")
		if len(domainEdges) == 0 {
			sb.WriteString("(none)\n")
		} else {
			for _, e := range domainEdges {
				fmt.Fprintf(&sb, "- %s -> %s\n", e.From, e.To)
			}
		}
		if core := largestCyclicCore(domainEdges); len(core) > 0 {
			fmt.Fprintf(&sb, "\nCyclic core (co-coupled; treat as one unit): %s\n", strings.Join(core, ", "))
		}
		sb.WriteString("\nWrite the architecture overview SKILL.md now.")
		return sb.String()
	}

	if maxBytes <= 0 {
		return build(ds)
	}
	// Drop domains from the end (alphabetically last, typically least central)
	// until the message fits the budget.
	for len(ds) > 1 {
		msg := build(ds)
		if len(msg) <= maxBytes {
			return msg
		}
		ds = ds[:len(ds)-1]
	}
	return build(ds)
}

// callProviderForText runs a single tool-less model turn and returns the first
// text block. tel may be nil. System prompt is empty (caller provides all
// context in the message).
func callProviderForText(ctx context.Context, prov provider.Provider, message string, tel *telemetry.Logger) (string, error) {
	tools := tool.NewRegistry()
	orch := orchestrator.New(prov, tools, tel, "", "codegen")
	text, err := orch.Chat(ctx, message)
	if err != nil {
		return "", fmt.Errorf("callProviderForText: %w", err)
	}
	return text, nil
}

// GenerateArchitecture runs one tool-less model turn and returns SKILL.md
// content. contextSize is the provider's token window (0 = no limit); the
// message is trimmed to fit before sending. The caller writes the file.
func GenerateArchitecture(ctx context.Context, project string, domains []DomainFact, domainEdges []Edge, prov provider.Provider, tel *telemetry.Logger, contextSize int) (string, error) {
	// Each token ≈ 4 bytes (conservative for code/paths). Reserve room for
	// system prompt and model output; the remainder is the message budget.
	maxBytes := 0
	if contextSize > 0 {
		available := contextSize - synthesisReservedOut - synthesisSystemTokens
		if available > 0 {
			maxBytes = available * 4
		}
	}
	tools := tool.NewRegistry()
	orch := orchestrator.New(prov, tools, tel, BuildSynthesisPrompt(), "learn-synthesize")
	content, err := orch.Chat(ctx, BuildSynthesisMessage(project, domains, domainEdges, maxBytes))
	if err != nil {
		return "", fmt.Errorf("codegen.GenerateArchitecture: %w", err)
	}
	return content, nil
}
