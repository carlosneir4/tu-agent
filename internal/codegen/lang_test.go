package codegen

import "testing"

func TestResolveLanguage(t *testing.T) {
	cases := []struct {
		name      string
		flag      string
		filePaths []string
		want      string
		wantErr   bool
	}{
		{"flag overrides detection", "go", []string{"a.java"}, "go", false},
		{"detect from files", "", []string{"a.go", "b.go", "c.java"}, "go", false},
		{"empty repo no flag errors", "", nil, "", true},
		{"empty repo with flag ok", "python", nil, "python", false},
		{"unknown flag rejected", "rust", []string{"a.go"}, "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveLanguage(tt.flag, tt.filePaths)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
