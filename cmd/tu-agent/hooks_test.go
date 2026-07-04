package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergePostToolUseHook_FreshFile(t *testing.T) {
	out, changed, err := mergePostToolUseHook(nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("merging into empty settings should report a change")
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON:\n%s", out)
	}
	s := string(out)
	for _, want := range []string{`"matcher": "Write|Edit"`, "tu-agent graph update --quiet"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestMergeInstallsBothHooks(t *testing.T) {
	out, changed, err := mergePostToolUseHook(nil)
	if err != nil || !changed {
		t.Fatalf("first merge: changed=%v err=%v, want true,nil", changed, err)
	}
	s := string(out)
	for _, want := range []string{
		`"matcher": "Write|Edit"`, "tu-agent graph update --quiet",
		`"matcher": "Bash"`, "tu-agent graph update --post-bash",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged settings missing %q\n%s", want, s)
		}
	}
	// Idempotent: a second merge over the produced output is a no-op.
	_, changed2, err := mergePostToolUseHook(out)
	if err != nil || changed2 {
		t.Fatalf("second merge: changed=%v err=%v, want false,nil", changed2, err)
	}
}

func TestMergePostToolUseHook_NonClobber(t *testing.T) {
	existing := []byte(`{
  "model": "qwen",
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo other"}]}
    ]
  }
}`)
	out, changed, err := mergePostToolUseHook(existing)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("adding our hooks to a file with a different Bash hook should be a change")
	}
	s := string(out)
	for _, want := range []string{
		`"model"`, "qwen", "echo other", "Bash",
		`"matcher": "Write|Edit"`, "tu-agent graph update --quiet",
		"tu-agent graph update --post-bash",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("non-clobber output missing %q:\n%s", want, s)
		}
	}
}

func TestMergePostToolUseHook_Idempotent(t *testing.T) {
	first, changed1, err := mergePostToolUseHook(nil)
	if err != nil || !changed1 {
		t.Fatalf("first merge: changed=%v err=%v", changed1, err)
	}
	second, changed2, err := mergePostToolUseHook(first)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if changed2 {
		t.Error("second merge must report no change")
	}
	if string(second) != string(first) {
		t.Errorf("second merge must be byte-identical:\n first=%s\nsecond=%s", first, second)
	}
}

func TestMergePostToolUseHook_MalformedShapesError(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"hooks not object", `{"hooks": "nope"}`},
		{"PostToolUse not array", `{"hooks": {"PostToolUse": "nope"}}`},
		{"matching entry inner hooks not array", `{"hooks": {"PostToolUse": [{"matcher": "Write|Edit", "hooks": "nope"}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, changed, err := mergePostToolUseHook([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for malformed input %q", tc.input)
			}
			if changed {
				t.Error("changed must be false on error")
			}
		})
	}
}
