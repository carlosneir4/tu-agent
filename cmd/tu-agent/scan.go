package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/skill"
	"github.com/tu/tu-agent/internal/subagent"
)

type skillRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Shadows     bool   `json:"shadows"`
}

type agentRow struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	ReadOnly bool   `json:"read_only"`
	Builtin  bool   `json:"builtin"`
}

type pluginRow struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Inventory is the effective set of resources a repo would use.
type Inventory struct {
	Skills  []skillRow           `json:"skills"`
	Agents  []agentRow           `json:"agents"`
	Plugins []pluginRow          `json:"plugins"`
	Routing config.RoutingConfig `json:"routing"`
}

// buildInventory assembles the effective inventory. Skill/agent search paths are
// ordered low->high precedence; later layers win and mark earlier ones shadowed.
func buildInventory(home, cwd string, routing config.RoutingConfig) Inventory {
	inv := Inventory{Routing: routing}

	// Skills: scan each layer separately to retain origin directory.
	skillDirs := skill.SearchPaths(home, cwd)
	winner := map[string]skillRow{}
	seenLower := map[string]bool{}
	for _, dir := range skillDirs {
		idx, err := skill.Scan([]string{dir})
		if err != nil {
			continue
		}
		for _, e := range idx.All() {
			if _, ok := winner[e.Name]; ok {
				seenLower[e.Name] = true
			}
			winner[e.Name] = skillRow{Name: e.Name, Description: e.Description, Source: dir}
		}
	}
	for name, row := range winner {
		row.Shadows = seenLower[name]
		inv.Skills = append(inv.Skills, row)
	}
	sort.Slice(inv.Skills, func(i, j int) bool { return inv.Skills[i].Name < inv.Skills[j].Name })

	// Agents: file-based per layer + built-ins.
	agentDirs := subagent.SearchPaths(home, cwd)
	roDirs := map[string]bool{}
	if home != "" {
		roDirs[filepath.Clean(filepath.Join(home, ".claude", "agents"))] = true
	}
	awin := map[string]agentRow{}
	for _, dir := range agentDirs {
		defs, err := subagent.Load([]string{dir}, roDirs)
		if err != nil {
			continue
		}
		for _, d := range defs {
			awin[d.Name] = agentRow{Name: d.Name, Source: dir, ReadOnly: roDirs[filepath.Clean(dir)]}
		}
	}
	for _, d := range subagent.BuiltinDefs() {
		if _, ok := awin[d.Name]; !ok {
			awin[d.Name] = agentRow{Name: d.Name, Source: "(built-in)", Builtin: true}
		}
	}
	for _, row := range awin {
		inv.Agents = append(inv.Agents, row)
	}
	sort.Slice(inv.Agents, func(i, j int) bool { return inv.Agents[i].Name < inv.Agents[j].Name })

	inv.Plugins = collectPlugins(home)
	return inv
}

// collectPlugins enumerates ~/.claude/plugins subdirectories for visibility.
// tu-agent does not execute these; listed so the user knows what Claude Code has.
func collectPlugins(home string) []pluginRow {
	if home == "" {
		return nil
	}
	root := filepath.Join(home, ".claude", "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var rows []pluginRow
	for _, e := range entries {
		if e.IsDir() {
			rows = append(rows, pluginRow{Name: e.Name(), Source: root})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func renderInventory(w io.Writer, inv Inventory, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(inv); err != nil {
			return fmt.Errorf("renderInventory: json: %w", err)
		}
		return nil
	}
	fmt.Fprintf(w, "SKILLS (%d)\n", len(inv.Skills))
	for _, s := range inv.Skills {
		note := ""
		if s.Shadows {
			note = "  (shadowed lower layer)"
		}
		fmt.Fprintf(w, "  %-28s %s%s\n", s.Name, s.Source, note)
	}
	fmt.Fprintf(w, "\nAGENTS (%d)\n", len(inv.Agents))
	for _, a := range inv.Agents {
		flags := ""
		if a.ReadOnly {
			flags += " (ro)"
		}
		fmt.Fprintf(w, "  %-28s %s%s\n", a.Name, a.Source, flags)
	}
	fmt.Fprintf(w, "\nPLUGINS (%d) — Claude Code plugins, not executed by tu-agent\n", len(inv.Plugins))
	for _, p := range inv.Plugins {
		fmt.Fprintf(w, "  %-28s %s\n", p.Name, p.Source)
	}
	fmt.Fprintf(w, "\nCONFIG\n  routing.default: %s\n", inv.Routing.Default)
	for task, prov := range inv.Routing.Tasks {
		fmt.Fprintf(w, "  routing.tasks.%s: %s\n", task, prov)
	}
	for ag, prov := range inv.Routing.SubAgents {
		fmt.Fprintf(w, "  routing.sub_agents.%s: %s\n", ag, prov)
	}
	return nil
}

var scanJSON bool

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "List the skills, agents, config, and plugins available to this repo",
	Long: `Read-only inventory of what tu-agent would use in the current repo:
skills and agents (effective, after layer precedence, with origin and shadowing),
the resolved routing config, and the Claude Code plugins installed (listed for
visibility — tu-agent does not execute them).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("scan: getwd: %w", err)
		}
		inv := buildInventory(home, cwd, cfg.Routing)
		return renderInventory(os.Stdout, inv, scanJSON)
	},
}

func init() {
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "emit the inventory as JSON")
}
