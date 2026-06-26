package main

import "testing"

func TestMCPToolNamesIncludesMutators(t *testing.T) {
	want := map[string]bool{"mem_rescope": false, "mem_delete": false}
	for _, n := range mcpToolNames {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("mcpToolNames missing %q", name)
		}
	}
}

func TestOrDefaultDefaultsEmptyToFallback(t *testing.T) {
	// Empty from_scope/scope must default to "project" so the common case
	// (move or delete a project note) works without specifying the source scope.
	if got := orDefault("", "project"); got != "project" {
		t.Errorf("orDefault(\"\", project) = %q, want project", got)
	}
	if got := orDefault("personal", "project"); got != "personal" {
		t.Errorf("orDefault(personal, project) = %q, want personal", got)
	}
}
