package tdd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckCoverage(t *testing.T) {
	if r := CheckCoverage([]string{"@s1", "@s2"}, []string{"@s2", "@s1"}); !r.OK {
		t.Fatalf("expected OK, got %+v", r)
	}
	// The @ prefix is normalized on both sides: covered tags without the @ still
	// match required tags that carry it (the plugin conductor often drops it).
	if r := CheckCoverage([]string{"@s1", "@s2"}, []string{"s1", "s2"}); !r.OK {
		t.Fatalf("expected OK with @-less covered tags, got %+v", r)
	}
	if r := CheckCoverage([]string{"s1", "s2"}, []string{"@s1", "@s2"}); !r.OK {
		t.Fatalf("expected OK with @-less required tags, got %+v", r)
	}
	r := CheckCoverage([]string{"@s1", "@s2"}, []string{"@s1"})
	if r.OK || r.Feedback == "" {
		t.Fatalf("expected not OK with feedback, got %+v", r)
	}
	if !strings.Contains(r.Feedback, "@s2") {
		t.Fatalf("feedback should name the missing tag in its original form, got %q", r.Feedback)
	}
}

func TestDeterministicJudge(t *testing.T) {
	green := func(context.Context) (bool, string, error) { return true, "ok", nil }
	red := func(context.Context) (bool, string, error) { return false, "FAIL: x", nil }
	boom := func(context.Context) (bool, string, error) { return false, "", errors.New("no runner") }

	if r := DeterministicJudge(context.Background(), green, []string{"@s1"}, []string{"@s1"}); !r.OK {
		t.Fatalf("green+covered should pass, got %+v", r)
	}
	if r := DeterministicJudge(context.Background(), green, []string{"@s1"}, nil); r.OK {
		t.Fatalf("missing coverage should fail")
	}
	if r := DeterministicJudge(context.Background(), red, []string{"@s1"}, []string{"@s1"}); r.OK {
		t.Fatalf("red tests should fail")
	}
	if r := DeterministicJudge(context.Background(), boom, []string{"@s1"}, []string{"@s1"}); r.OK {
		t.Fatalf("runner error should fail")
	}
}
