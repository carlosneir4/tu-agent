package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSetupInDir(t *testing.T, dir string, stdinLines string) error {
	t.Helper()
	in := strings.NewReader(stdinLines)
	var out bytes.Buffer
	return runSetup(in, &out, filepath.Join(dir, "config.yaml"))
}

func TestSetup_WritesConfigWithDefaults(t *testing.T) {
	dir := t.TempDir()
	err := runSetupInDir(t, dir, "http://localhost:1234\n\n\n\n")
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"http://localhost:1234",
		"qwen/qwen3-coder-30b",
		"16384",
		"0.2",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
}

func TestSetup_OverwritesWhenConfirmed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("existing: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runSetupInDir(t, dir, "y\nhttp://myserver:1234\n\n\n\n")
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(data), "existing: true") {
		t.Error("expected old config to be replaced")
	}
	if !strings.Contains(string(data), "http://myserver:1234") {
		t.Error("expected new base_url in config")
	}
}

func TestSetup_AbortsWhenNotConfirmed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("existing: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runSetupInDir(t, dir, "n\n")
	if err == nil {
		t.Fatal("expected error when user aborts")
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "existing: true") {
		t.Error("expected original config to be preserved on abort")
	}
}

func TestSetup_CustomValues(t *testing.T) {
	dir := t.TempDir()
	err := runSetupInDir(t, dir, "http://gpu:8080\nmy/custom-model\n8192\n0.5\n")
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	content := string(data)
	for _, want := range []string{"http://gpu:8080", "my/custom-model", "8192", "0.5"} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
}

func TestRunSetupHooks_FreshRepo(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runSetupHooks(dir, &out); err != nil {
		t.Fatalf("runSetupHooks: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	for _, want := range []string{hookMatcher, hookCommand} {
		if !strings.Contains(string(data), want) {
			t.Errorf("settings.json missing %q:\n%s", want, data)
		}
	}
	if !strings.Contains(out.String(), "Installed") {
		t.Errorf("expected install confirmation, got %q", out.String())
	}
}

func TestRunSetupHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	var out1 bytes.Buffer
	if err := runSetupHooks(dir, &out1); err != nil {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(dir, ".claude", "settings.json")
	first, _ := os.ReadFile(path)

	var out2 bytes.Buffer
	if err := runSetupHooks(dir, &out2); err != nil {
		t.Fatalf("second: %v", err)
	}
	second, _ := os.ReadFile(path)

	if string(second) != string(first) {
		t.Errorf("second run changed the file:\n first=%s\nsecond=%s", first, second)
	}
	if !strings.Contains(out2.String(), "already installed") {
		t.Errorf("expected 'already installed' notice, got %q", out2.String())
	}
}
