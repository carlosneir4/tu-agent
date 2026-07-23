package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTddPluginSkillWiresTrustCheck pins the Step 0 trust wiring in the tdd
// conductor skill: Step 0 must read the trust decision (`tdd trust --check`),
// show the command text with the scope note (trust covers the command text,
// "not the executables" it invokes), and record trust on confirm
// (`tdd trust --yes`).
func TestTddPluginSkillWiresTrustCheck(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "tdd", "SKILL.md"))
	if err != nil {
		t.Fatalf("read tdd SKILL.md: %v", err)
	}
	s := string(raw)
	for _, want := range []string{"tdd trust --check", "not the executables", "tdd trust --yes"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md missing trust-wiring anchor %q", want)
		}
	}
}
