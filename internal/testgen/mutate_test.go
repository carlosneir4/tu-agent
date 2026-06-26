package testgen

import "strings"

import "testing"

func TestAnnotateMutationGoEOF(t *testing.T) {
	src := "package store\n\nfunc TestStoreSave_Gen(t *testing.T) {}\n"
	out := AnnotateMutation("go", src, "score 60% — 2 survivors")
	if !strings.Contains(out, "// MUTATION: score 60% — 2 survivors") {
		t.Fatalf("missing MUTATION note:\n%s", out)
	}
	// Idempotent: re-annotating replaces, not duplicates.
	out2 := AnnotateMutation("go", out, "score 80% — 1 survivor")
	if strings.Count(out2, "// MUTATION:") != 1 {
		t.Fatalf("expected one MUTATION line, got:\n%s", out2)
	}
	if !strings.Contains(out2, "80%") || strings.Contains(out2, "60%") {
		t.Errorf("note not replaced:\n%s", out2)
	}
}

func TestAnnotateMutationSentinel(t *testing.T) {
	src := "import pytest\n# tu-agent:gen:start\ndef test_x_gen():\n    pass\n# tu-agent:gen:end\n"
	out := AnnotateMutation("python", src, "score 50%")
	if !strings.Contains(out, "# MUTATION: score 50%") {
		t.Fatalf("missing MUTATION note inside region:\n%s", out)
	}
	// The note sits before the end sentinel.
	if strings.Index(out, "# MUTATION:") > strings.Index(out, genEnd) {
		t.Errorf("MUTATION note must be inside the gen region:\n%s", out)
	}
}
