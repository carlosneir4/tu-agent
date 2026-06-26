package extract

import (
	"reflect"
	"testing"
)

func TestExtensions_sortedAndComplete(t *testing.T) {
	got := Extensions()
	want := []string{".go", ".graphql", ".java", ".py", ".ts", ".tsx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Extensions() = %v, want %v", got, want)
	}
}

func TestParserFor(t *testing.T) {
	cases := map[string]bool{
		"Foo.java": true, "main.go": true, "svc.py": true,
		"app.ts": true, "App.tsx": true, "schema.graphql": true,
		"README.md": false, "noext": false,
	}
	for path, wantFound := range cases {
		if got := parserFor(path) != nil; got != wantFound {
			t.Errorf("parserFor(%q) found=%v, want %v", path, got, wantFound)
		}
	}
}
