package tdd_test

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

// Component 3: tdd.TestCommandTrust(cmd, autogen, store, root) -> (trusted, reason).
// These tests exercise the production symbol via calls; they are deliberately
// named TestTrustDecision_* so Go's test tooling never confuses them with the
// production function named TestCommandTrust (which lives in a non-_test.go
// file and is therefore not treated as a test).

// The store-independent branches (empty, matches-autogen, untrusted) need no
// disk: an empty tdd.Store{} is safe because HashFor tolerates a nil map.
func TestTrustDecision_StoreIndependentBranches(t *testing.T) {
	cases := []struct {
		name             string // scenario id + intent
		cmd              string
		autogen          string
		root             string
		wantTrusted      bool
		wantReasonExact  string // asserted verbatim when non-empty
		wantReasonSubstr string // asserted as a substring when non-empty
	}{
		{
			name:            "@s1 whitespace-only command is trusted as empty",
			cmd:             "   ",
			autogen:         "", // irrelevant for this branch
			root:            "/anything",
			wantTrusted:     true,
			wantReasonExact: "empty",
		},
		{
			name:            "@s2 command equal to autogen is trusted",
			cmd:             "go test ./...",
			autogen:         "go test ./...",
			root:            "/anything",
			wantTrusted:     true,
			wantReasonExact: "matches autogen",
		},
		{
			name:        "@s4 differing, unstored command is untrusted",
			cmd:         "make test",
			autogen:     "go test ./...",
			root:        "/anything",
			wantTrusted: false,
		},
		{
			name:             "@s5 untrusted reason names the autogen command",
			cmd:              "make test",
			autogen:          "go test ./...",
			root:             "/anything",
			wantTrusted:      false,
			wantReasonSubstr: "go test ./...",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			autogen := func() string { return tc.autogen }

			trusted, reason := tdd.TestCommandTrust(tc.cmd, autogen, tdd.Store{}, tc.root)

			if trusted != tc.wantTrusted {
				t.Fatalf("trusted = %v, want %v (reason=%q)", trusted, tc.wantTrusted, reason)
			}
			if tc.wantReasonExact != "" && reason != tc.wantReasonExact {
				t.Fatalf("reason = %q, want exactly %q", reason, tc.wantReasonExact)
			}
			if tc.wantReasonSubstr != "" && !strings.Contains(reason, tc.wantReasonSubstr) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tc.wantReasonSubstr)
			}
		})
	}
}

// @s3: a command absent from autogen but whose hash was previously confirmed
// for the root is trusted. The Store is built through the real
// SaveTrust -> LoadStore round-trip (Component 2), so this also proves the two
// components compose. No t.Parallel: t.Setenv forbids it.
func TestTrustDecision_StoredHashIsPreviouslyConfirmed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const root = "/repo"
	const cmd = "make test"

	// Record the command for the root, then load the persisted store back.
	if err := tdd.SaveTrust(root, cmd); err != nil {
		t.Fatalf("SaveTrust: %v", err)
	}
	path, err := tdd.TrustPath()
	if err != nil {
		t.Fatalf("TrustPath: %v", err)
	}
	store := tdd.LoadStore(path)

	// autogen deliberately returns a DIFFERENT command so branch 2 cannot match;
	// only the stored-hash branch (3) can make this trusted.
	autogen := func() string { return "go test ./..." }

	trusted, reason := tdd.TestCommandTrust(cmd, autogen, store, root)

	if !trusted {
		t.Fatalf("trusted = false, want true (reason=%q)", reason)
	}
	if reason != "previously confirmed" {
		t.Fatalf("reason = %q, want %q", reason, "previously confirmed")
	}
}
