package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

// TestGraphProtocolMentionsAllTools guards against drift between the injected
// graph MCP tools and the protocol prose that tells the agent to use them: every
// tool in GraphAgentTools must be referenced (by short name) in graphProtocolBody.
func TestGraphProtocolMentionsAllTools(t *testing.T) {
	for _, full := range codegen.GraphAgentTools() {
		short := full[strings.LastIndex(full, "__")+2:]
		if !strings.Contains(graphProtocolBody, short) {
			t.Errorf("graphProtocolBody does not mention tool %q (short %q)", full, short)
		}
	}
}

func writeAgent(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAugmentAgents_InjectsSpecialistsIntoGeneralists(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeAgent(t, dir, "developer.md", "---\nname: acme-developer\ndescription: \"Implements features.\"\ntools: Read, Write, Bash\n---\nSenior developer.\n")
	writeAgent(t, dir, "qa.md", "---\nname: acme-qa\ndescription: \"Test strategy.\"\ntools: Read, Write\n---\nQA.\n")
	writeAgent(t, dir, "nextjs-expert.md", "---\nname: nextjs-expert\ndescription: \"Expert for Next.js apps\"\ntools: Read, Write\n---\nNext.js expert.\n")

	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}

	dev, _ := os.ReadFile(filepath.Join(dir, "developer.md"))
	if !strings.Contains(string(dev), codegen.SpecialistsOpen) {
		t.Errorf("developer missing specialists block:\n%s", dev)
	}
	if !strings.Contains(string(dev), "nextjs-expert") || !strings.Contains(string(dev), "Expert for Next.js apps") {
		t.Errorf("developer specialists block missing the custom agent:\n%s", dev)
	}

	qa, _ := os.ReadFile(filepath.Join(dir, "qa.md"))
	if !strings.Contains(string(qa), codegen.SpecialistsOpen) {
		t.Errorf("qa missing specialists block:\n%s", qa)
	}

	ex, _ := os.ReadFile(filepath.Join(dir, "nextjs-expert.md"))
	if strings.Contains(string(ex), codegen.SpecialistsOpen) {
		t.Errorf("specialist should not get a specialists block:\n%s", ex)
	}

	// Idempotent: a second run leaves exactly one block.
	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}
	dev2, _ := os.ReadFile(filepath.Join(dir, "developer.md"))
	if n := strings.Count(string(dev2), codegen.SpecialistsOpen); n != 1 {
		t.Errorf("expected exactly one specialists block after re-run, got %d", n)
	}
}

func TestAugmentAgents_NoSpecialistsNoBlock(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeAgent(t, dir, "developer.md", "---\nname: acme-developer\ndescription: \"Implements features.\"\ntools: Read, Write\n---\nDev.\n")

	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}
	dev, _ := os.ReadFile(filepath.Join(dir, "developer.md"))
	if strings.Contains(string(dev), codegen.SpecialistsOpen) {
		t.Errorf("no custom specialists exist; developer must not get a block:\n%s", dev)
	}
}

func TestAugmentAgents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeAgent(t, dir, "domain-expert.md", "---\nname: domain-expert\ntools: Read, Write, Bash\n---\nYou are a domain expert.\n\n## Custom\nKeep me.\n")
	writeAgent(t, dir, "planner.md", "---\nname: planner\n---\nYou plan.\n")

	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}

	d1, _ := os.ReadFile(filepath.Join(dir, "domain-expert.md"))
	s1 := string(d1)
	if !strings.Contains(s1, "mcp__tu-agent-graph__get_context") {
		t.Errorf("domain-expert tools not unioned:\n%s", s1)
	}
	if !strings.Contains(s1, "tu-agent:graph-protocol") {
		t.Errorf("domain-expert missing protocol block:\n%s", s1)
	}
	if !strings.Contains(s1, "Keep me.") {
		t.Errorf("custom body lost:\n%s", s1)
	}

	d2, _ := os.ReadFile(filepath.Join(dir, "planner.md"))
	if !strings.Contains(string(d2), "tools: mcp__tu-agent-graph__get_context") {
		t.Errorf("planner tools line not created:\n%s", string(d2))
	}

	// idempotent: a second run leaves domain-expert byte-identical
	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}
	d1b, _ := os.ReadFile(filepath.Join(dir, "domain-expert.md"))
	if string(d1b) != s1 {
		t.Errorf("not idempotent:\n--first--\n%s\n--second--\n%s", s1, string(d1b))
	}
}

func TestAugmentAgents_Exclude(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeAgent(t, dir, "skip-me.md", "---\nname: skip-me\ntools: Read\n---\nbody\n")
	if err := augmentAgents(root, map[string]bool{"skip-me": true}); err != nil {
		t.Fatal(err)
	}
	d, _ := os.ReadFile(filepath.Join(dir, "skip-me.md"))
	if strings.Contains(string(d), "mcp__tu-agent-graph") {
		t.Errorf("excluded agent was augmented:\n%s", string(d))
	}
}

func TestAugmentAgents_NoDir(t *testing.T) {
	if err := augmentAgents(t.TempDir(), nil); err != nil {
		t.Errorf("missing agents dir should be a no-op, got %v", err)
	}
}

func TestAugmentAgents_SkipsNonInlineTools(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")

	jsonArrContent := "---\nname: json-arr\ntools: [\"Write\", \"Read\"]\n---\nbody\n"
	blockListContent := "---\nname: block-list\ntools:\n  - Read\n  - Write\n---\nbody\n"

	writeAgent(t, dir, "json-arr.md", jsonArrContent)
	writeAgent(t, dir, "block-list.md", blockListContent)

	if err := augmentAgents(root, nil); err != nil {
		t.Fatal(err)
	}

	// JSON array agent must be byte-identical to input (no corruption, no injection)
	d1, _ := os.ReadFile(filepath.Join(dir, "json-arr.md"))
	if string(d1) != jsonArrContent {
		t.Errorf("json-arr agent was modified:\ngot:  %q\nwant: %q", string(d1), jsonArrContent)
	}
	if strings.Contains(string(d1), "mcp__tu-agent-graph") {
		t.Errorf("json-arr agent has graph tools injected (corrupted):\n%s", string(d1))
	}
	if strings.Contains(string(d1), "tu-agent:graph-protocol") {
		t.Errorf("json-arr agent has protocol block injected (should be skipped):\n%s", string(d1))
	}

	// YAML block-list agent must be byte-identical to input
	d2, _ := os.ReadFile(filepath.Join(dir, "block-list.md"))
	if string(d2) != blockListContent {
		t.Errorf("block-list agent was modified:\ngot:  %q\nwant: %q", string(d2), blockListContent)
	}
	if strings.Contains(string(d2), "mcp__tu-agent-graph") {
		t.Errorf("block-list agent has graph tools injected (corrupted):\n%s", string(d2))
	}
	if strings.Contains(string(d2), "tu-agent:graph-protocol") {
		t.Errorf("block-list agent has protocol block injected (should be skipped):\n%s", string(d2))
	}
}

func TestRunInitSetup_AugmentAgents(t *testing.T) {
	dir := t.TempDir()
	adir := filepath.Join(dir, ".claude", "agents")
	writeAgent(t, adir, "x.md", "---\nname: x\ntools: Read\n---\nbody\n")
	t.Chdir(dir)

	if err := runInitSetup(context.Background(), initSetupOpts{AugmentAgents: true}); err != nil {
		t.Fatalf("runInitSetup augment: %v", err)
	}
	d, _ := os.ReadFile(filepath.Join(adir, "x.md"))
	if !strings.Contains(string(d), "mcp__tu-agent-graph__get_context") {
		t.Errorf("augment via runInitSetup did not run:\n%s", string(d))
	}
}
