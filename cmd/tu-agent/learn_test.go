package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/graph/store"
	"github.com/tu/tu-agent/internal/provider"
)

// The post-learn reminder must tell the user to start a fresh session (or
// /clear) AND say why — so the just-written CLAUDE.md protocol takes effect.
// The first session that ran learn answered structural questions with grep
// because the protocol wasn't yet in its loaded context; this reminder closes
// that gap.
func TestLearnCompletionReminder_TellsUserToStartFreshSession(t *testing.T) {
	msg := learnCompletionReminder()
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "/clear") && !strings.Contains(lower, "new session") {
		t.Errorf("reminder must give the actionable fresh-session instruction; got:\n%s", msg)
	}
	if !strings.Contains(lower, "claude.md") && !strings.Contains(lower, "protocol") {
		t.Errorf("reminder must explain why (CLAUDE.md protocol takes effect); got:\n%s", msg)
	}
}

func TestRunLearnConceptPhaseWritesCards(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".tu-agent"), 0o755)
	st, err := store.Open(filepath.Join(dir, ".tu-agent", "graph.db"), "test-ext")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	units := []codegen.SourceUnit{
		{Path: "src/c/P.java", Package: "com.acme.shop.catalog"},
		{Path: "src/o/O.java", Package: "com.acme.shop.orders"},
	}
	cards, err := buildConceptCardsFromUnits(units, nil, nil, []string{"com.acme.shop"}, codegen.DomainMapOptions{Depth: 1, MinFiles: 1}, "leiden")
	if err != nil {
		t.Fatal(err)
	}
	if err := persistConceptCardsTo(st, cards); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"catalog", "orders"} {
		row, ok, gerr := st.GetConcept(name)
		if gerr != nil || !ok {
			t.Fatalf("GetConcept(%s): ok=%v err=%v", name, ok, gerr)
		}
		if len(row.Content) > 1024 {
			t.Errorf("card %s exceeds 1KB: %d", name, len(row.Content))
		}
	}
}

func TestExecuteInitChunk_WritesToClaudeSkills(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "svc.go"), []byte("package svc\n// cache layer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	skillsDir := generatedSkillsDir(".")
	expected := filepath.Join(".", ".claude", "skills")
	if skillsDir != expected {
		t.Errorf("generatedSkillsDir = %q, want %q", skillsDir, expected)
	}
	if filepath.Base(filepath.Dir(skillsDir)) == ".tu-agent" {
		t.Errorf("skillsDir must not be under .tu-agent, got %q", skillsDir)
	}
	_ = dir
}

func TestLearnCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"provider", "depth", "min-files", "max-files-per-domain", "patterns",
		"skip-llm", "cluster", "concept-root"} {
		if learnCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s to be registered on learnCmd", name)
		}
	}
}

func TestLearnCmd_DefaultDomainFlags(t *testing.T) {
	if f := learnCmd.Flags().Lookup("depth"); f == nil || f.DefValue != "1" {
		t.Errorf("depth default = %v, want 1", f)
	}
	if f := learnCmd.Flags().Lookup("min-files"); f == nil || f.DefValue != "5" {
		t.Errorf("min-files default = %v, want 5", f)
	}
	if f := learnCmd.Flags().Lookup("max-files-per-domain"); f == nil || f.DefValue != "0" {
		t.Errorf("max-files-per-domain default = %v, want 0 (budget-based splitting is the default)", f)
	}
}

func TestLearnCmd_AcceptsOptionalPositionalArg(t *testing.T) {
	if err := learnCmd.Args(learnCmd, []string{}); err != nil {
		t.Errorf("0 args should be allowed: %v", err)
	}
	if err := learnCmd.Args(learnCmd, []string{"some/path"}); err != nil {
		t.Errorf("1 arg should be allowed: %v", err)
	}
	if err := learnCmd.Args(learnCmd, []string{"a", "b"}); err == nil {
		t.Error("2 args should be rejected")
	}
}

func TestLearn_KnowledgeBlockRegistered(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "svc.go"), []byte("package svc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# proj\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	skillsDir := filepath.Join(dir, ".claude", "skills", "cache")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: cache\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeKnowledgeBlock(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("writeKnowledgeBlock: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(md), "<!-- tu-agent:knowledge -->") {
		t.Errorf("expected knowledge block in CLAUDE.md:\n%s", md)
	}
}

func TestMaxFilesFlagDefaultIsZero(t *testing.T) {
	f := learnCmd.Flags().Lookup("max-files-per-domain")
	if f == nil {
		t.Fatal("flag missing")
	}
	if f.DefValue != "0" {
		t.Errorf("max-files-per-domain default = %s, want 0 (budget-based splitting is the default)", f.DefValue)
	}
}

// writeGeneratedSkill seeds one valid generated skill so registerPhase proceeds.
func writeGeneratedSkill(t *testing.T, root string) {
	t.Helper()
	d := filepath.Join(root, ".claude", "skills", "feed")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: feed\ndescription: feed domain\n---\n# Feed\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterPhase_RunsSynthesize(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	writeGeneratedSkill(t, dir)

	runSynthesizeAndEnrich(context.Background(), learnOpts{})
}

func TestRegisterPhase_SynthesizeFailureDoesNotAbort(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	writeGeneratedSkill(t, dir)

	runSynthesizeAndEnrich(context.Background(), learnOpts{})
	registerStatic()

	md, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not written despite synthesize failure: %v", err)
	}
	if !strings.Contains(string(md), "<!-- tu-agent:knowledge -->") {
		t.Errorf("knowledge block missing:\n%s", md)
	}
}

func TestDryRunSplitsByBudget(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("// filler\n", 4000)
	writeFile := func(name, pkg string) {
		p := filepath.Join(root, "src", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "package " + pkg + ";\npublic class " + strings.TrimSuffix(filepath.Base(name), ".java") + " {}\n" + big
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("Alpha.java", "com.acme.widget")
	writeFile("Beta.java", "com.acme.widget")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	s, err := openGraphStore()
	if err != nil {
		t.Fatal(err)
	}
	units, edges, _, err := loadSourceUnits(s)
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	domains := codegen.BuildDomainMap(units, edges, codegen.DomainMapOptions{
		Depth: 1, MaxBytes: 50000,
	})
	var leafDomains []codegen.Domain
	for _, d := range domains {
		if d.Files != nil {
			leafDomains = append(leafDomains, d)
		}
	}
	if len(leafDomains) != 2 {
		t.Errorf("expected 2 byte-batched leaf domains, got %+v", domains)
	}
}

func TestRunLearn_SkipLLM_NoModelCall(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, ".claude", "skills", "core")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: core\ndescription: core\n---\n\nKey files:\n- src/A.java\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runLearn(context.Background(), learnOpts{SkipLLM: true}); err != nil {
		t.Fatalf("runLearn --skip-llm: %v", err)
	}
	md, err := os.ReadFile("CLAUDE.md")
	if err != nil || !strings.Contains(string(md), "tu-agent:knowledge") {
		t.Fatalf("knowledge block missing: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(".tu-agent", "graph.db")); err != nil {
		t.Errorf("graph.db missing: %v", err)
	}
}

type countingMergedProvider struct{ calls int }

func (p *countingMergedProvider) Send(ctx context.Context, system string, messages []provider.Message, tools []provider.ToolDef) (provider.Response, error) {
	p.calls++
	text := "=== ARCHITECTURE ===\n# Arch\nmap body\n" +
		"=== PROJECT-CONTEXT ===\n## Coding Conventions\n- wrap errors\n## Key Entry Points\n- src/A.java — entry\n"
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: text}}, StopReason: "end_turn"}, nil
}
func (p *countingMergedProvider) Name() string             { return "merged" }
func (p *countingMergedProvider) Model() string            { return "merged" }
func (p *countingMergedProvider) NativeContextWindow() int { return 0 }

func TestRunSynthesizeAndEnrich_MergedSingleCall(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatal(err)
	}

	// Seed concept via the store (not disk).
	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "core", Description: "core", Content: "---\nname: core\ndescription: core\n---\n\nKey files:\n- src/A.java\n"},
	}); err != nil {
		t.Fatalf("seed concepts: %v", err)
	}
	st.Close()

	p := &countingMergedProvider{}
	mergedSynthesizeAndEnrich(context.Background(), ".", p, 0)

	if p.calls != 1 {
		t.Errorf("merged path should make exactly 1 model call, made %d", p.calls)
	}
	archPath := filepath.Join(root, ".claude", "skills", "architecture", "SKILL.md")
	arch, err := os.ReadFile(archPath)
	if err != nil || !strings.Contains(string(arch), "map body") {
		t.Errorf("architecture skill not written from merged call: err=%v", err)
	}
	md, _ := os.ReadFile("CLAUDE.md")
	if !strings.Contains(string(md), "## Coding Conventions") {
		t.Errorf("project-context block not upserted from merged call:\n%s", md)
	}
}
