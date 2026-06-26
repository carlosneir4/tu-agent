package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writePkg(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectConceptRoots_arrayForm(t *testing.T) {
	dir := t.TempDir()
	writePkg(t, dir, `{"workspaces":["packages/*","rigs/jest","rigs/eslint"]}`)
	if got := detectConceptRoots(dir); !reflect.DeepEqual(got, []string{"packages", "rigs"}) {
		t.Fatalf("array form = %v, want [packages rigs]", got)
	}
}

func TestDetectConceptRoots_objectForm(t *testing.T) {
	dir := t.TempDir()
	writePkg(t, dir, `{"workspaces":{"packages":["packages/*","native-packages/*"],"nohoist":["**/x"]}}`)
	if got := detectConceptRoots(dir); !reflect.DeepEqual(got, []string{"native-packages", "packages"}) {
		t.Fatalf("object form = %v, want [native-packages packages]", got)
	}
}

func TestDetectConceptRoots_none(t *testing.T) {
	if got := detectConceptRoots(t.TempDir()); got != nil {
		t.Fatalf("no package.json → nil, got %v", got)
	}
	dir := t.TempDir()
	writePkg(t, dir, `{"name":"x"}`)
	if got := detectConceptRoots(dir); got != nil {
		t.Fatalf("no workspaces → nil, got %v", got)
	}
}

func TestResolveConceptRoots_precedence(t *testing.T) {
	saved := cfg
	defer func() { cfg = saved }()

	// Explicit wins (flag ∪ config plural ∪ singular, deduped), no auto-detect.
	cfg.Learn.ConceptRoots = []string{"rigs"}
	cfg.Learn.ConceptRoot = "packages"
	dir := t.TempDir()
	writePkg(t, dir, `{"workspaces":["should-not-matter/*"]}`)
	got := resolveConceptRoots([]string{"packages"}, dir) // "packages" also from singular → deduped
	want := []string{"packages", "rigs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit precedence = %v, want %v", got, want)
	}

	// No explicit → auto-detect from workspaces.
	cfg.Learn.ConceptRoots = nil
	cfg.Learn.ConceptRoot = ""
	if got := resolveConceptRoots(nil, dir); !reflect.DeepEqual(got, []string{"should-not-matter"}) {
		t.Fatalf("auto-detect = %v, want [should-not-matter]", got)
	}

	// No explicit, no workspaces → nil (domain-map fallback).
	if got := resolveConceptRoots(nil, t.TempDir()); got != nil {
		t.Fatalf("fallback = %v, want nil", got)
	}
}
