package tdd

// RED-phase test for @s8 of feature design-approval-token (spec.md D4,
// design.md Feature 1): the RED gate warns — never blocks — when a begun
// run's state.json records a non-trivial complexity and no approval token
// exists. GateResult.Warning is an existing exported field (never set on the
// red path today), and RunGate's signature is unchanged, so this test
// compiles cleanly against today's code; it is red purely because the RED
// branch never populates Warning yet.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// @s8: a begun standard-complexity run with no approval token gets a
// non-empty, approval-mentioning Warning from a RED gate run, while the
// gate's own ok verdict (red genuinely confirmed) is unaffected.
func TestDesignApprovalS8_RedGateWarnsWithoutToken(t *testing.T) {
	root := t.TempDir()
	writeFeature(t, root) // count.feature with tags @s1,@s2, precedent from gatecmd_test.go

	base := tddDir(root)
	stateRaw := `{"version":1,"task":"t","features":[{"name":"count","status":"pending"}],"complexity":"standard"}`
	if err := os.WriteFile(filepath.Join(base, "state.json"), []byte(stateRaw), 0o644); err != nil {
		t.Fatalf("write state.json fixture: %v", err)
	}
	// Deliberately no progress/design-approval.json written: the token is absent.

	redCfg := config.Config{Tdd: config.TddConfig{TestCommand: "false"}} // a failing check: the RED gate confirms red
	ctx := context.Background()

	res, err := RunGate(ctx, redCfg, root, "", "count", "", "red", "", "", testRunnerResolver)
	if err != nil {
		t.Fatalf("RunGate: %v", err)
	}
	if !res.OK {
		t.Fatalf("want the red confirmation itself unaffected by the warning (ok=true), got: %+v", res)
	}
	if res.Warning == "" {
		t.Errorf("want a non-empty Warning on a begun standard run without an approval token, got empty: %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.Warning), "approv") {
		t.Errorf("Warning %q should mention approval", res.Warning)
	}
}
