package main

import (
	"errors"
	"strings"
	"testing"
)

func TestMutatesTree(t *testing.T) {
	mutating := []string{
		"rm foo", "rm -rf build", "  rm x", "ls && rm y", "cat a | rm b",
		"mv a b", "patch < p.diff",
		"git checkout .", "git reset --hard", "git restore src/x.go",
		"git stash", "git clean -fd", "git apply p.patch", "git revert HEAD", "git rm x",
		"git switch main", "git merge feature", "git rebase main", "git pull",
	}
	for _, c := range mutating {
		if !mutatesTree(c) {
			t.Errorf("mutatesTree(%q) = false, want true", c)
		}
	}
	nonMutating := []string{
		"ls", "cat file", "git status", "git log --oneline", "git diff",
		"go test ./...", "echo confirm", "grep rm file", "npm run build", "terraform plan",
	}
	for _, c := range nonMutating {
		if mutatesTree(c) {
			t.Errorf("mutatesTree(%q) = true, want false", c)
		}
	}
}

func TestPostBashDecision(t *testing.T) {
	// A mutating command triggers reconcile.
	called := false
	err := postBashDecision(
		strings.NewReader(`{"tool_input":{"command":"git checkout ."}}`),
		func() error { called = true; return nil },
	)
	if err != nil || !called {
		t.Fatalf("mutating: called=%v err=%v, want true,nil", called, err)
	}

	// A non-mutating command does not.
	called = false
	err = postBashDecision(
		strings.NewReader(`{"tool_input":{"command":"go test ./..."}}`),
		func() error { called = true; return nil },
	)
	if err != nil || called {
		t.Fatalf("non-mutating: called=%v err=%v, want false,nil", called, err)
	}

	// Empty and garbage stdin are safe no-ops (never fail the hook).
	for _, in := range []string{"", "   ", "not json", `{"tool_input":{}}`} {
		called = false
		if err := postBashDecision(strings.NewReader(in), func() error { called = true; return nil }); err != nil {
			t.Fatalf("input %q: err=%v, want nil", in, err)
		}
		if called {
			t.Fatalf("input %q: reconcile called, want skip", in)
		}
	}
}

func TestPostBashDecisionSwallowsReconcileError(t *testing.T) {
	// Reconcile errors must be swallowed (logged to stderr, return nil).
	called := false
	err := postBashDecision(
		strings.NewReader(`{"tool_input":{"command":"git reset --hard"}}`),
		func() error { called = true; return errors.New("boom") },
	)
	if err != nil || !called {
		t.Fatalf("err=%v called=%v, want nil,true", err, called)
	}
}
