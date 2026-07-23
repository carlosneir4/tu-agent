package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/tdd"
)

// newTrustRepo builds a temp repo whose autogen test command is deterministic
// ("go test ./..." for a bare go.mod) and points tdd.TrustPath() into a fresh
// temp HOME. It returns the repo dir and the resolved trust.json path.
//
// No t.Parallel: t.Setenv (HOME) forbids it.
func newTrustRepo(t *testing.T) (repo, storePath string) {
	t.Helper()
	repo = t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/x\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// Guard: the fixture must resolve to the expected autogen command, or the
	// scenarios below are testing the wrong thing.
	if got := codegen.DetectTestCommandForRoot(repo); got != "go test ./..." {
		t.Fatalf("autogen precondition: got %q, want %q", got, "go test ./...")
	}

	t.Setenv("HOME", t.TempDir())
	p, err := tdd.TrustPath()
	if err != nil {
		t.Fatalf("TrustPath: %v", err)
	}
	return repo, p
}

// @s1: --check on a trusted repo prints trusted JSON and writes nothing.
func TestTrustCheck_Trusted(t *testing.T) {
	repo, storePath := newTrustRepo(t)
	configCmd := "go test ./..." // equals autogen for the go.mod repo

	st := computeTrustStatus(repo, configCmd, storePath)

	var buf bytes.Buffer
	if err := runTrustCheck(&buf, st); err != nil {
		t.Fatalf("runTrustCheck: %v", err)
	}

	var got trustStatus
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, buf.String())
	}
	if !got.Trusted {
		t.Errorf("trusted: got false, want true (config == autogen)\nJSON: %s", buf.String())
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Errorf("trust.json must not be written by --check; os.Stat err = %v", err)
	}
}

// @s2: --check on an untrusted repo reports the divergence.
func TestTrustCheck_Untrusted(t *testing.T) {
	repo, storePath := newTrustRepo(t)
	configCmd := "make weird" // differs from autogen "go test ./..."

	st := computeTrustStatus(repo, configCmd, storePath)

	var buf bytes.Buffer
	if err := runTrustCheck(&buf, st); err != nil {
		t.Fatalf("runTrustCheck: %v", err)
	}

	var got trustStatus
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, buf.String())
	}
	if !got.Differs {
		t.Errorf("differs: got false, want true (config != autogen)\nJSON: %s", buf.String())
	}
	if got.AutogenCommand != "go test ./..." {
		t.Errorf("autogen_command: got %q, want %q", got.AutogenCommand, "go test ./...")
	}
}

// @s3: --yes on an untrusted repo remembers the command; a following --check
// then reports trusted.
func TestTrustYes_ThenCheckTrusted(t *testing.T) {
	repo, storePath := newTrustRepo(t)
	configCmd := "make weird"

	var buf bytes.Buffer
	if err := runTrustYes(&buf, repo, configCmd, storePath); err != nil {
		t.Fatalf("runTrustYes: %v", err)
	}

	st2 := computeTrustStatus(repo, configCmd, storePath)
	if !st2.Trusted {
		t.Errorf("after --yes, trusted: got false, want true (command should be remembered)")
	}
}

// @s4: --yes on an already-trusted repo is a no-op: it states nothing to do and
// leaves trust.json byte-unchanged.
func TestTrustYes_AlreadyTrustedNoop(t *testing.T) {
	repo, storePath := newTrustRepo(t)
	configCmd := "make weird"

	if err := tdd.SaveTrust(repo, configCmd); err != nil {
		t.Fatalf("pre-seed SaveTrust: %v", err)
	}
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read seeded trust.json: %v", err)
	}

	var buf bytes.Buffer
	if err := runTrustYes(&buf, repo, configCmd, storePath); err != nil {
		t.Fatalf("runTrustYes: %v", err)
	}

	msg := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("nothing")) && !bytes.Contains(buf.Bytes(), []byte("already")) {
		t.Errorf("expected a nothing-to-do message, got: %q", msg)
	}

	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read trust.json after no-op: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("trust.json changed on no-op --yes:\nbefore: %s\nafter:  %s", before, after)
	}
}

// @s5: a bare non-TTY invocation behaves like --check — it prints status JSON
// and does not block on a prompt. A *bytes.Buffer stdin reads as non-TTY.
func TestTrustCmd_BareNonTTYIsCheck(t *testing.T) {
	newTrustRepo(t) // sets HOME so trust resolution is hermetic

	var buf bytes.Buffer
	tddTrustCmd.SetOut(&buf)
	tddTrustCmd.SetIn(bytes.NewBufferString("")) // non-*os.File reader => non-TTY

	if err := tddTrustCmd.RunE(tddTrustCmd, []string{}); err != nil {
		t.Fatalf("RunE returned error (should route to check path): %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, buf.String())
	}
	if _, ok := got["trusted"]; !ok {
		t.Errorf("status JSON missing \"trusted\" field: %s", buf.String())
	}
}

// @s6: the warning prints the command verbatim and the scope note.
func TestTrustWarning_VerbatimAndScope(t *testing.T) {
	w := trustWarning("./gradlew test", "go test ./...")

	if !bytes.Contains([]byte(w), []byte("./gradlew test")) {
		t.Errorf("warning missing verbatim config command:\n%s", w)
	}
	if !bytes.Contains([]byte(w), []byte("not the executables")) {
		t.Errorf("warning missing scope note \"not the executables\":\n%s", w)
	}
}
