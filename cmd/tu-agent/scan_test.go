package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

func mkSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildInventory_SkillShadowing(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	// Same skill name in ~/.claude/skills and ./.claude/skills — project wins.
	mkSkill(t, filepath.Join(home, ".claude", "skills"), "cache", "home version")
	mkSkill(t, filepath.Join(cwd, ".claude", "skills"), "cache", "project version")
	mkSkill(t, filepath.Join(cwd, ".tu-agent", "skills"), "solr", "project solr")

	inv := buildInventory(home, cwd, config.RoutingConfig{Default: "claude"})

	got := map[string]skillRow{}
	for _, s := range inv.Skills {
		got[s.Name] = s
	}
	if got["cache"].Source != filepath.Join(cwd, ".claude", "skills") {
		t.Errorf("cache should resolve to project .claude layer, got %q", got["cache"].Source)
	}
	if !got["cache"].Shadows {
		t.Errorf("cache should be marked as shadowing a lower layer")
	}
	if got["solr"].Shadows {
		t.Errorf("solr appears once, should not shadow")
	}
}

func mkAgent(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: d\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildInventory_AgentReadOnly(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	// Project-local agents are untrusted and must be reported read-only;
	// home-layer agents are trusted and must not be.
	mkAgent(t, filepath.Join(home, ".claude", "agents"), "home-agent")
	mkAgent(t, filepath.Join(cwd, ".claude", "agents"), "project-agent")

	inv := buildInventory(home, cwd, config.RoutingConfig{Default: "claude"})

	got := map[string]agentRow{}
	for _, a := range inv.Agents {
		got[a.Name] = a
	}
	if !got["project-agent"].ReadOnly {
		t.Errorf("project-agent (untrusted cwd dir) should be reported ReadOnly=true, got %+v", got["project-agent"])
	}
	if got["home-agent"].ReadOnly {
		t.Errorf("home-agent (trusted home dir) should be reported ReadOnly=false, got %+v", got["home-agent"])
	}
}

func TestRenderInventory_JSON(t *testing.T) {
	inv := Inventory{
		Skills:  []skillRow{{Name: "cache", Description: "d", Source: "/x/.claude/skills"}},
		Routing: config.RoutingConfig{Default: "claude"},
	}
	var buf bytes.Buffer
	if err := renderInventory(&buf, inv, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"name": "cache"`) {
		t.Errorf("json missing skill name:\n%s", buf.String())
	}
}

func TestRenderInventory_Human(t *testing.T) {
	inv := Inventory{
		Skills:  []skillRow{{Name: "cache", Source: "/x/.claude/skills", Shadows: true}},
		Agents:  []agentRow{{Name: "codebase-explorer", Source: "(built-in)", Builtin: true}},
		Plugins: []pluginRow{{Name: "superpowers", Source: "/h/.claude/plugins"}},
		Routing: config.RoutingConfig{Default: "claude"},
	}
	var buf bytes.Buffer
	if err := renderInventory(&buf, inv, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"SKILLS", "cache", "shadowed", "AGENTS", "codebase-explorer", "PLUGINS", "not executed by tu-agent", "routing.default: claude"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
}
