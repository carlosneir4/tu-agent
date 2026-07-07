package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestResolvePrivate(t *testing.T) {
	cases := []struct {
		name            string
		private, public bool
		want            bool
	}{
		{"default (no flags) is private", false, false, true},
		{"--public opts into shared gitignore", false, true, false},
		{"--private stays private", true, false, true},
		{"--private wins over --public (safe)", true, true, true},
	}
	for _, tc := range cases {
		if got := resolvePrivate(tc.private, tc.public); got != tc.want {
			t.Errorf("%s: resolvePrivate(%v,%v) = %v, want %v", tc.name, tc.private, tc.public, got, tc.want)
		}
	}
}

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

func TestGenerateAgentsBacksUpExisting(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "SENTINEL: hand-tuned developer agent, do not lose me"
	developerPath := filepath.Join(agentsDir, "developer.md")
	if err := os.WriteFile(developerPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	if err := generateAgents(info, "go", "go build ./...", "go test ./...", true); err != nil {
		t.Fatalf("generateAgents: %v", err)
	}

	bak, err := os.ReadFile(developerPath + ".bak")
	if err != nil {
		t.Fatalf("expected developer.md.bak to exist: %v", err)
	}
	if string(bak) != sentinel {
		t.Errorf("developer.md.bak = %q, want sentinel %q", bak, sentinel)
	}

	regenerated, err := os.ReadFile(developerPath)
	if err != nil {
		t.Fatalf("expected developer.md to exist: %v", err)
	}
	if string(regenerated) == sentinel {
		t.Error("developer.md should have been regenerated, not left as the sentinel content")
	}
}

// TestGenerateAgentsForceFalseDoesNotTouchBackup: when force=false,
// writeAgentFile skips the write entirely, so generateAgents must not touch
// dest.bak — otherwise a meaningful older backup (from a prior --force run)
// gets clobbered with a redundant copy while nothing is actually written.
func TestGenerateAgentsForceFalseDoesNotTouchBackup(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const current = "current hand-tuned developer agent"
	const oldBackup = "OLD BACKUP: from a prior --force run, must survive untouched"
	developerPath := filepath.Join(agentsDir, "developer.md")
	if err := os.WriteFile(developerPath, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(developerPath+".bak", []byte(oldBackup), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	if err := generateAgents(info, "go", "go build ./...", "go test ./...", false); err != nil {
		t.Fatalf("generateAgents: %v", err)
	}

	bak, err := os.ReadFile(developerPath + ".bak")
	if err != nil {
		t.Fatalf("expected developer.md.bak to still exist: %v", err)
	}
	if string(bak) != oldBackup {
		t.Errorf("developer.md.bak = %q, want untouched old backup %q", bak, oldBackup)
	}

	unchanged, err := os.ReadFile(developerPath)
	if err != nil {
		t.Fatalf("expected developer.md to exist: %v", err)
	}
	if string(unchanged) != current {
		t.Errorf("developer.md = %q, want unchanged (write should have been skipped, force=false)", unchanged)
	}
}

// TestGenerateAgentsBackupFailureAbortsOverwrite: if the backup write itself
// fails (simulated here by making dest.bak an existing directory), the
// overwrite must be aborted with an error and the live agent file must be
// left unchanged — mirroring applyHardening's hard-fail-before-overwrite
// behavior for settings.json.
func TestGenerateAgentsBackupFailureAbortsOverwrite(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "SENTINEL: hand-tuned developer agent, do not lose me"
	developerPath := filepath.Join(agentsDir, "developer.md")
	if err := os.WriteFile(developerPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the backup destination an existing directory so os.WriteFile onto
	// it fails.
	if err := os.MkdirAll(developerPath+".bak", 0o755); err != nil {
		t.Fatal(err)
	}

	info := &codegen.ProjectInfo{Name: "example-project"}
	err := generateAgents(info, "go", "go build ./...", "go test ./...", true)
	if err == nil {
		t.Fatal("expected generateAgents to return an error when backup write fails")
	}

	unchanged, readErr := os.ReadFile(developerPath)
	if readErr != nil {
		t.Fatalf("expected developer.md to still exist: %v", readErr)
	}
	if string(unchanged) != sentinel {
		t.Errorf("developer.md = %q, want unchanged sentinel %q (overwrite must be aborted on backup failure)", unchanged, sentinel)
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
	// Keep: the test-command one-liner and tu-agent's marked blocks.
	for _, want := range []string{"go test ./...", "tu-agent:project-context", "tu-agent:knowledge"} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, s)
		}
	}
	// Slimmed: tu-agent no longer writes unmarked project prose that would clash
	// with Claude's native /init (which owns the overview/structure/conventions).
	for _, gone := range []string{"## Project Structure", "## Working agreement"} {
		if strings.Contains(s, gone) {
			t.Errorf("CLAUDE.md must not contain unmarked prose %q (it clashes with native /init):\n%s", gone, s)
		}
	}
}
