package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/skill"
)

func TestIndex_AddGet(t *testing.T) {
	idx := skill.New()
	e := skill.Entry{Name: "my-skill", Description: "does stuff", Path: "/tmp/SKILL.md"}
	idx.Add(e)

	got, ok := idx.Get("my-skill")
	if !ok {
		t.Fatal("expected to find my-skill")
	}
	if got.Name != "my-skill" || got.Description != "does stuff" {
		t.Errorf("unexpected entry: %+v", got)
	}
}

func TestIndex_Get_Missing(t *testing.T) {
	idx := skill.New()
	_, ok := idx.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing key")
	}
}

func TestIndex_Add_Overwrites(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "s", Description: "v1", Path: "/a"})
	idx.Add(skill.Entry{Name: "s", Description: "v2", Path: "/b"})
	got, _ := idx.Get("s")
	if got.Description != "v2" {
		t.Errorf("expected v2, got %q", got.Description)
	}
}

func TestIndex_All_Sorted(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "z-skill", Description: "z"})
	idx.Add(skill.Entry{Name: "a-skill", Description: "a"})
	idx.Add(skill.Entry{Name: "m-skill", Description: "m"})

	all := idx.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	if all[0].Name != "a-skill" || all[1].Name != "m-skill" || all[2].Name != "z-skill" {
		t.Errorf("not sorted: %v", all)
	}
}

func TestIndex_Len(t *testing.T) {
	idx := skill.New()
	if idx.Len() != 0 {
		t.Fatal("expected 0")
	}
	idx.Add(skill.Entry{Name: "s1"})
	idx.Add(skill.Entry{Name: "s2"})
	if idx.Len() != 2 {
		t.Fatalf("expected 2, got %d", idx.Len())
	}
}

func TestIndex_Summary_Empty(t *testing.T) {
	idx := skill.New()
	if idx.Summary() != "" {
		t.Errorf("expected empty summary, got %q", idx.Summary())
	}
}

func TestIndex_Summary_Format(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "bash-helper", Description: "runs bash scripts"})
	idx.Add(skill.Entry{Name: "api-client", Description: "calls REST APIs"})

	want := "- api-client: calls REST APIs\n- bash-helper: runs bash scripts"
	if idx.Summary() != want {
		t.Errorf("summary mismatch\ngot:  %q\nwant: %q", idx.Summary(), want)
	}
}

func TestIndex_LoadContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: test\ndescription: a test\n---\n# Test Skill\nDoes stuff."
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := skill.New()
	idx.Add(skill.Entry{Name: "test", Description: "a test", Path: path})

	got, err := idx.LoadContent("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch\ngot:  %q\nwant: %q", got, content)
	}
}

func TestIndex_LoadContent_NotFound(t *testing.T) {
	idx := skill.New()
	_, err := idx.LoadContent("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing skill")
	}
}

func TestIndex_LoadContent_FileGone(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "gone", Description: "d", Path: "/nonexistent/SKILL.md"})
	_, err := idx.LoadContent("gone")
	if err == nil {
		t.Fatal("expected error when file is missing")
	}
}
