package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteKnowledgeBlock_InsertsOnce(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(md, []byte("# Proj\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeKnowledgeBlock(md); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, _ := os.ReadFile(md)
	if c := strings.Count(string(first), "<!-- tu-agent:knowledge -->"); c != 1 {
		t.Fatalf("want 1 open marker, got %d", c)
	}
	// Second call must not duplicate the block.
	if err := writeKnowledgeBlock(md); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, _ := os.ReadFile(md)
	if c := strings.Count(string(second), "<!-- tu-agent:knowledge -->"); c != 1 {
		t.Errorf("want 1 open marker after re-run, got %d", c)
	}
	if string(first) != string(second) {
		t.Errorf("re-run changed file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(string(second), "get_architecture") {
		t.Errorf("block missing get_architecture reference:\n%s", second)
	}
}

func TestWriteKnowledgeBlock_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	if err := writeKnowledgeBlock(md); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(md); err != nil {
		t.Errorf("expected CLAUDE.md created: %v", err)
	}
}

func TestUpsertMarkedBlock_InsertThenReplace(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(md, []byte("# Title\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	open, close := "<!-- x:start -->", "<!-- x:end -->"
	body1 := open + "\nFIRST\n" + close
	if err := upsertMarkedBlock(md, open, close, body1); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(md)
	if !strings.Contains(string(got), "FIRST") || !strings.Contains(string(got), "# Title") {
		t.Fatalf("insert failed: %q", got)
	}
	body2 := open + "\nSECOND\n" + close
	if err := upsertMarkedBlock(md, open, close, body2); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(md)
	if strings.Contains(string(got), "FIRST") {
		t.Errorf("replace failed, FIRST still present: %q", got)
	}
	if strings.Count(string(got), "x:start") != 1 {
		t.Errorf("block duplicated: %q", got)
	}
}

func TestKnowledgeBody_DocumentsBrainArtifacts(t *testing.T) {
	for _, want := range []string{
		"get_architecture",
		"architecture",
		"## PROTOCOL",
		"get_context",
		"get_impact",
		"find_symbol",
		"tu-agent graph context",
		"The graph MCP tools are often DEFERRED",
	} {
		if !strings.Contains(knowledgeBody, want) {
			t.Errorf("knowledgeBody missing %q", want)
		}
	}
}

func TestKnowledgeBlockMentionsGetConceptNotDomainSkill(t *testing.T) {
	block := knowledgeBody // const string in knowledge_block.go that renders the CLAUDE.md block
	if strings.Contains(block, "domain skill") {
		t.Errorf("knowledge block still references per-concept domain skills:\n%s", block)
	}
	if !strings.Contains(block, "get_concept") {
		t.Errorf("knowledge block should point to get_concept for concept meaning")
	}
}

func TestKnowledgeBodyHasMemoryAndVerify(t *testing.T) {
	for _, want := range []string{
		"## MEMORY",
		"mem_recent",
		"mem_save",
		"tu-agent memory import",
		"## VERIFY before claiming done",
		"## If the graph looks wrong",
		"tu-agent learn",
		"OUTSIDE the tu-agent:knowledge markers",
		"type `gotcha`",
		"memory search --type gotcha",
		"## Communication — explain plainly",
		"Gloss every acronym, jargon term, or coined name on first use",
	} {
		if !strings.Contains(knowledgeBody, want) {
			t.Errorf("knowledgeBody missing %q", want)
		}
	}
}

func TestKnowledgeBody_DocumentsContentConvention(t *testing.T) {
	for _, want := range []string{
		"Symptom/trigger", "Root cause", "Prevention",
		"name the code symbols",
	} {
		if !strings.Contains(knowledgeBody, want) {
			t.Errorf("knowledgeBody missing content-convention guidance %q", want)
		}
	}
}

func TestKnowledgeBlockMatchesSynthesizer(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", "architecture-synthesizer.md"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(knowledgeOpen) + `.*?` + regexp.QuoteMeta(knowledgeClose))
	got := re.FindString(string(raw))
	if got == "" {
		t.Fatal("knowledge block not found in synthesizer agent .md")
	}
	if got != knowledgeBody {
		t.Errorf("synthesizer block differs from knowledgeBody const.\n--- const ---\n%s\n--- synthesizer ---\n%s", knowledgeBody, got)
	}
}

func TestUpsertMarkedBlockLiteralDollar(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(md, []byte("# Title\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	open, close := "<!-- x:start -->", "<!-- x:end -->"
	body := open + "\n" + "`$BASE` and $1 and $100\n" + close
	if err := upsertMarkedBlock(md, open, close, body); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	// Re-run (upsert replaces an existing block): must stay byte-identical, and
	// the $1-style token must survive — regexp.ReplaceAllString would treat it
	// as a submatch reference and silently eat it.
	if err := upsertMarkedBlock(md, open, close, body); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("re-run changed file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(string(second), "$1") {
		t.Errorf("literal $1 token was eaten by re-run:\n%s", second)
	}
}

func TestKnowledgeBody_HasGroundworkDirective(t *testing.T) {
	for _, want := range []string{
		"## GROUNDWORK",
		"`groundwork` skill",
		"anchor",
		"`tdd` dev-flow",
	} {
		if !strings.Contains(knowledgeBody, want) {
			t.Errorf("knowledgeBody missing groundwork directive substring %q", want)
		}
	}
}
