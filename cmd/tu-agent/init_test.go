package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseExtensions_LeadingDot(t *testing.T) {
	got := parseExtensions(".go,.java,.py")
	want := []string{".go", ".java", ".py"}
	if len(got) != len(want) {
		t.Fatalf("parseExtensions() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseExtensions_NoDot(t *testing.T) {
	got := parseExtensions("go,java")
	for _, ext := range got {
		if ext[0] != '.' {
			t.Errorf("extension missing dot: %q", ext)
		}
	}
}

func TestParseExtensions_Trims(t *testing.T) {
	got := parseExtensions(" .go , .java ")
	if len(got) != 2 {
		t.Fatalf("expected 2 extensions, got %v", got)
	}
}

func TestParseExtensions_SkipsEmpty(t *testing.T) {
	got := parseExtensions(".go,,,.java")
	if len(got) != 2 {
		t.Fatalf("expected 2 extensions (empty entries skipped), got %v", got)
	}
}

func TestParseExtensions_Lowercase(t *testing.T) {
	got := parseExtensions(".GO,.Java")
	for _, ext := range got {
		if ext != strings.ToLower(ext) {
			t.Errorf("extension should be lowercase: %q", ext)
		}
	}
}

func TestWriteAgentFile_WritesWhenNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "developer.md")
	skipped, err := writeAgentFile(path, "content", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Error("expected file to be written, not skipped")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "content" {
		t.Errorf("unexpected content: %q", data)
	}
}

func TestWriteAgentFile_SkipsWhenExistsAndNoForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "developer.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	skipped, err := writeAgentFile(path, "new content", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skipped {
		t.Error("expected file to be skipped when it already exists and force=false")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Error("original content must not be overwritten when force=false")
	}
}

func TestWriteAgentFile_OverwritesWhenForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "developer.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	skipped, err := writeAgentFile(path, "new content", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Error("expected file to be written when force=true")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("expected new content with force=true, got %q", data)
	}
}

func TestWriteAgentFile_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "developer.md")
	_, err := writeAgentFile(path, "content", false)
	if err != nil {
		t.Fatalf("expected parent dir creation, got error: %v", err)
	}
}

func TestInitCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"lang", "no-llm", "force"} {
		if initCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s to be registered on initCmd", name)
		}
	}
}

func TestInitCmd_AcceptsOptionalPositionalArg(t *testing.T) {
	// 0 args allowed (current behavior)
	if err := initCmd.Args(initCmd, []string{}); err != nil {
		t.Errorf("0 args should be allowed: %v", err)
	}
	// 1 arg allowed (new behavior)
	if err := initCmd.Args(initCmd, []string{"src/main"}); err != nil {
		t.Errorf("1 arg should be allowed: %v", err)
	}
	// 2 args rejected
	if err := initCmd.Args(initCmd, []string{"a", "b"}); err == nil {
		t.Error("2 args should be rejected")
	}
}

func TestInitSetupOnly_NoSkills(t *testing.T) {
	dir := t.TempDir()
	// Create a minimal Go repo
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Change cwd to temp dir
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "", NoLLM: true, Force: false}); err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "developer.md")); err != nil {
		t.Errorf("expected developer agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Errorf("expected CLAUDE.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("init must not create .claude/skills, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tu-agent", "skills")); !os.IsNotExist(err) {
		t.Errorf("init must not create .tu-agent/skills, stat err=%v", err)
	}
}

func TestInitSetup_EmptyRepoRequiresLang(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	err := runInitSetup(context.Background(), initSetupOpts{NoLLM: true})
	if err == nil {
		t.Fatal("expected error for empty repo without --lang")
	}
}

func TestInitSetup_EmptyRepoWithLang(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go", NoLLM: true}); err != nil {
		t.Fatalf("expected success with --lang go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "developer.md")); err != nil {
		t.Errorf("expected agents generated from template: %v", err)
	}
}

func TestRunInitSetup_Deterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// No provider configured / no network: must still succeed.
	if err := runInitSetup(context.Background(), initSetupOpts{Force: true}); err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}
	md, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	for _, want := range []string{"go test ./...", "tu-agent:project-context", "Project Structure"} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, s)
		}
	}
}
