package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tu/tu-agent/internal/codegen"
)

// mapDomain is one entry of `tu-agent map --json` output. Context is the
// same structural block (inbound/outbound dependencies) that the binary's
// own learn pipeline feeds to skill generation.
type mapDomain struct {
	Name    string   `json:"name"`
	Package string   `json:"package"`
	Parent  string   `json:"parent,omitempty"`
	Files   []string `json:"files,omitempty"`
	Context string   `json:"context,omitempty"`
}

// mapOutput is the top-level `tu-agent map --json` document.
type mapOutput struct {
	Domains []mapDomain `json:"domains"`
}

// runMap builds the graph store, loads source units, clusters them into
// domains, and renders the result. Pure orchestration; no model calls.
func runMap(subpath string, depth, minFiles, maxFiles, maxBytes, minStandalone int, cluster string, jsonOut bool) (string, error) {
	if err := runGraphBuild(subpath); err != nil {
		return "", fmt.Errorf("map: building graph: %w", err)
	}
	s, err := openGraphStore()
	if err != nil {
		return "", fmt.Errorf("map: opening store: %w", err)
	}
	units, edges, weighted, err := loadSourceUnits(s)
	s.Close()
	if err != nil {
		return "", err
	}
	if len(units) == 0 {
		return "", fmt.Errorf("map: no source files found under %q", subpath)
	}
	domains, err := buildDomains(units, edges, weighted, codegen.DomainMapOptions{
		Depth: depth, MinFiles: minFiles, MaxFiles: maxFiles, MaxBytes: maxBytes,
		MinStandaloneFiles: minStandalone,
	}, cluster)
	if err != nil {
		return "", err
	}

	if !jsonOut {
		var sb strings.Builder
		for _, d := range domains {
			switch {
			case d.Files == nil:
				fmt.Fprintf(&sb, "%s (parent) — %s\n", d.Name, d.Package)
			case d.Parent != "":
				fmt.Fprintf(&sb, "  %s (%d files) — %s\n", d.Name, len(d.Files), d.Package)
			default:
				fmt.Fprintf(&sb, "%s (%d files) — %s\n", d.Name, len(d.Files), d.Package)
			}
		}
		return sb.String(), nil
	}

	out := mapOutput{Domains: make([]mapDomain, 0, len(domains))}
	for _, d := range domains {
		md := mapDomain{Name: d.Name, Package: d.Package, Parent: d.Parent, Files: d.Files}
		if d.Files != nil {
			md.Context = codegen.BuildDomainContext(d, units, edges)
		}
		out.Domains = append(out.Domains, md)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("map: marshalling: %w", err)
	}
	return string(data) + "\n", nil
}

// runMapPlan prints a cost preview of a learn run: domain counts, per-domain
// size, estimated LLM calls (one per leaf domain) and estimated input tokens
// (bytes/3). No model calls are made.
func runMapPlan(subpath string, depth, minFiles, maxFiles, maxBytes, minStandalone int, cluster string) (string, error) {
	if err := runGraphBuild(subpath); err != nil {
		return "", fmt.Errorf("map: building graph: %w", err)
	}
	s, err := openGraphStore()
	if err != nil {
		return "", fmt.Errorf("map: opening store: %w", err)
	}
	units, edges, weighted, err := loadSourceUnits(s)
	s.Close()
	if err != nil {
		return "", err
	}
	if len(units) == 0 {
		return "", fmt.Errorf("map: no source files found under %q", subpath)
	}
	domains, err := buildDomains(units, edges, weighted, codegen.DomainMapOptions{
		Depth: depth, MinFiles: minFiles, MaxFiles: maxFiles, MaxBytes: maxBytes,
		MinStandaloneFiles: minStandalone,
	}, cluster)
	if err != nil {
		return "", err
	}

	sizeOf := make(map[string]int, len(units))
	for _, u := range units {
		sizeOf[u.Path] = u.Size
	}
	var sb strings.Builder
	var topLevel, subDomains, llmCalls, totalBytes int
	for _, d := range domains {
		if d.Files == nil {
			topLevel++ // parent marker: rendered deterministically, no LLM call
			continue
		}
		var dBytes int
		for _, p := range d.Files {
			dBytes += sizeOf[p]
		}
		totalBytes += dBytes
		llmCalls++
		if d.Parent == "" {
			topLevel++
			fmt.Fprintf(&sb, "%-40s %4d files  ~%6d tokens\n", d.Name, len(d.Files), dBytes/3)
		} else {
			subDomains++
			fmt.Fprintf(&sb, "  %-38s %4d files  ~%6d tokens\n", d.Name, len(d.Files), dBytes/3)
		}
	}
	fmt.Fprintf(&sb, "\ntop-level domains: %d   sub-domains: %d\n", topLevel, subDomains)
	fmt.Fprintf(&sb, "LLM calls: %d\n", llmCalls)
	fmt.Fprintf(&sb, "estimated input tokens: ~%d\n", totalBytes/3)
	if topLevel > 30 {
		fmt.Fprintf(&sb, "WARNING: %d top-level domains exceed the 30-skill index budget; raise --max-files-per-domain or accept deeper references\n", topLevel)
	}
	return sb.String(), nil
}

var (
	mapDepth         int
	mapMinFiles      int
	mapMaxFiles      int
	mapMaxBytes      int
	mapMinStandalone int
	mapCluster       string
	mapPlan          bool
	mapJSON          bool
)

var mapCmd = &cobra.Command{
	Use:   "map [path]",
	Short: "Print the domain map (deterministic, no model calls)",
	Long: `Clusters source files into domains exactly as 'tu-agent learn' would,
and prints the result. With --json, each domain includes its structural
context (inbound/outbound dependencies) so external orchestrators — like
the Claude Code plugin — can generate skills without re-deriving anything.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := ""
		if len(args) == 1 {
			sub = args[0]
		}
		maxFiles := mapMaxFiles
		if !cmd.Flags().Changed("max-files-per-domain") && cfg.Learn.MaxFilesPerDomain > 0 {
			maxFiles = cfg.Learn.MaxFilesPerDomain
		}
		minStandalone := mapMinStandalone
		if !cmd.Flags().Changed("min-standalone-files") && cfg.Learn.MinStandaloneFiles > 0 {
			minStandalone = cfg.Learn.MinStandaloneFiles
		}
		if mapPlan {
			out, err := runMapPlan(sub, mapDepth, mapMinFiles, maxFiles, mapMaxBytes, minStandalone, mapCluster)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}
		out, err := runMap(sub, mapDepth, mapMinFiles, maxFiles, mapMaxBytes, minStandalone, mapCluster, mapJSON)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

func init() {
	mapCmd.Flags().IntVar(&mapDepth, "depth", 1, "package segments below the common root that define a domain")
	mapCmd.Flags().IntVar(&mapMinFiles, "min-files", 5, "domains smaller than this merge into the most-coupled sibling")
	mapCmd.Flags().IntVar(&mapMaxFiles, "max-files-per-domain", 0, "split domains larger than this (0 = config default, 40)")
	mapCmd.Flags().IntVar(&mapMaxBytes, "max-bytes-per-domain", 0, "split domains whose source exceeds this byte budget (0 = off); set to match 'tu-agent learn' when used with --json")
	mapCmd.Flags().IntVar(&mapMinStandalone, "min-standalone-files", 0, "packages with this many non-test files never merge (0 = config default, 4)")
	mapCmd.Flags().StringVar(&mapCluster, "cluster", "leiden", "domain clustering strategy: leiden (topology) or heuristic (package path)")
	mapCmd.Flags().BoolVar(&mapPlan, "plan", false, "print learn cost preview (domains, LLM calls, est. tokens) without model calls")
	mapCmd.Flags().BoolVar(&mapJSON, "json", false, "emit machine-readable JSON with structural context")
}
