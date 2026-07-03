package tdd

import (
	"strings"
	"testing"
)

func TestMutationTargetFromContract(t *testing.T) {
	tests := []struct {
		name     string
		artifact []Artifact
		wantOK   bool
		wantLang string
		wantDir  string
	}{
		{"go", []Artifact{{Kind: "source", Path: "internal/foo/bar.go"}}, true, "go", "internal/foo"},
		{"java", []Artifact{{Kind: "source", Path: "src/main/java/App.java"}}, true, "java", "src/main/java"},
		{"python", []Artifact{{Kind: "source", Path: "pkg/x.py"}}, true, "python", "pkg"},
		{"typescript", []Artifact{{Kind: "source", Path: "web/a.ts"}}, true, "typescript", "web"},
		{"no source artifact", []Artifact{{Kind: "doc", Path: "README.md"}}, false, "", ""},
		{"unknown extension", []Artifact{{Kind: "source", Path: "lib/x.rb"}}, false, "", ""},
		{"empty", nil, false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt, ok := MutationTargetFromContract(Contract{Artifacts: tt.artifact})
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if mt.Language != tt.wantLang || mt.Dir != tt.wantDir {
				t.Fatalf("got {%q,%q}, want {%q,%q}", mt.Language, mt.Dir, tt.wantLang, tt.wantDir)
			}
		})
	}
}

func TestMutationTargetFromArtifact(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
		wantOK   bool
		wantLang string
		wantDir  string
	}{
		{"go source", Artifact{Kind: "source", Path: "internal/foo/bar.go"}, true, "go", "internal/foo"},
		{"java source", Artifact{Kind: "source", Path: "src/main/java/App.java"}, true, "java", "src/main/java"},
		{"kind test", Artifact{Kind: "test", Path: "internal/foo/bar_test.go"}, false, "", ""},
		{"unknown extension", Artifact{Kind: "source", Path: "lib/x.rb"}, false, "", ""},
		{"empty path", Artifact{Kind: "source", Path: ""}, false, "", ""},
		{"zero value", Artifact{}, false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt, ok := MutationTargetFromArtifact(tt.artifact)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if mt.Language != tt.wantLang || mt.Dir != tt.wantDir {
				t.Fatalf("got {%q,%q}, want {%q,%q}", mt.Language, mt.Dir, tt.wantLang, tt.wantDir)
			}
		})
	}
}

func TestMutationGate(t *testing.T) {
	if got := MutationGate(0.7, MutationOutcome{Skipped: true}); !got.OK {
		t.Fatalf("skipped outcome must be OK (advisory), got %+v", got)
	}
	if got := MutationGate(0.7, MutationOutcome{Score: 0.7}); !got.OK {
		t.Fatalf("score == threshold must be OK, got %+v", got)
	}
	if got := MutationGate(0.7, MutationOutcome{Score: 0.95}); !got.OK {
		t.Fatalf("score > threshold must be OK, got %+v", got)
	}
	got := MutationGate(0.7, MutationOutcome{Score: 0.2, Survivors: []string{"x.go:10 if->true"}})
	if got.OK {
		t.Fatalf("score < threshold must not be OK")
	}
	if !strings.Contains(got.Feedback, "x.go:10 if->true") {
		t.Fatalf("feedback must list survivors, got %q", got.Feedback)
	}
}
