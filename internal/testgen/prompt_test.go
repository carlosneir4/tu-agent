package testgen

import (
	"strings"
	"testing"
)

func sampleGenContext() *GenContext {
	return &GenContext{
		Target: Target{
			Name: "Store.Save", Path: "internal/store/store.go",
			Params: "(v string)", ReturnType: "error",
		},
		PackageClause: "package store",
		Body:          "func (s *Store) Save(v string) error {\n\treturn s.db.Put(v)\n}",
		CallSites: []CallSite{
			{Caller: "cmd/main.go:42 (run)", Snippet: "if err := st.Save(input); err != nil {"},
		},
		Callees:      []string{"internal/store/db.go:10 DB.Put(v string) error"},
		SkillExcerpt: "Store persists via DB.Put.",
		BlastRadius:  7,
	}
}

func TestBuildGenerationPrompt(t *testing.T) {
	got := BuildGenerationPrompt(sampleGenContext(), "FRAGMENT-MARKER", "internal/store/store_gen_test.go")
	for _, want := range []string{
		"Store.Save", "package store", "s.db.Put", // target + body
		"cmd/main.go:42", "st.Save(input)", // call site
		"DB.Put", // callee
		"Store persists", "FRAGMENT-MARKER",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generation prompt missing %q", want)
		}
	}
}

func TestBuildRepairPrompt(t *testing.T) {
	got := BuildRepairPrompt(sampleGenContext(), "FRAGMENT-MARKER", "internal/store/store_gen_test.go",
		"package store\nfunc TestStoreSave(t *testing.T) {}", "--- FAIL: TestStoreSave")
	for _, want := range []string{
		"FRAGMENT-MARKER", "Previous attempt", "--- FAIL: TestStoreSave",
		"source under test is correct",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
		wantErr  bool
	}{
		{
			name:     "fenced with language",
			response: "Here you go:\n```go\npackage x\n```\nDone.",
			want:     "package x\n",
		},
		{
			name:     "bare file no fence",
			response: "package x\n\nimport \"testing\"\n",
			want:     "package x\n\nimport \"testing\"\n",
		},
		{name: "prose only", response: "I cannot write that test.", wantErr: true},
		{name: "empty fence", response: "```go\n\n```", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractCode(tt.response)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
