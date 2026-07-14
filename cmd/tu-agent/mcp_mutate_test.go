package main

import "testing"

func TestMCPToolNamesIncludesMutators(t *testing.T) {
	t.Chdir(t.TempDir())
	served := servedToolNames(t)
	for _, name := range []string{"mem_rescope", "mem_delete"} {
		if !served[name] {
			t.Errorf("newMCPServer does not serve %q", name)
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
