package testgen

import (
	"strings"
	"testing"
)

func TestSnakeCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ParseGo", "parse_go"},
		{"Store.Save", "store_save"},
		{"HTTPServer", "http_server"},
		{"parse_file", "parse_file"},
		{"Store.save_all", "store_save_all"},
	}
	for _, tt := range tests {
		if got := snakeCase(tt.in); got != tt.want {
			t.Errorf("snakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPythonAdapterDetect(t *testing.T) {
	a := &PythonAdapter{}
	root := t.TempDir()
	err := a.Detect(root)
	if err == nil {
		t.Fatal("Detect on empty dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--dry-run") {
		t.Fatalf("Detect error must point at --dry-run, got: %v", err)
	}
	writeFiles(t, root, "pyproject.toml")
	if err := a.Detect(root); err != nil {
		t.Fatalf("Detect with pyproject.toml: %v", err)
	}

	root2 := t.TempDir()
	writeFiles(t, root2, "requirements.txt")
	if err := a.Detect(root2); err != nil {
		t.Fatalf("Detect with requirements.txt: %v", err)
	}
}

func TestPythonAdapterTestPath(t *testing.T) {
	a := &PythonAdapter{}
	tgt := Target{Name: "parse_line", Path: "pkg/parser.py", Language: "python"}

	t.Run("no tests dir: sibling", func(t *testing.T) {
		root := t.TempDir()
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "pkg/test_parser.py" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("tests dir: mirror with src stripped", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, "tests/.keep")
		srcTgt := Target{Name: "parse_line", Path: "src/pkg/parser.py", Language: "python"}
		got, err := a.TestPath(root, srcTgt)
		if err != nil || got != "tests/pkg/test_parser.py" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("tests dir: bare src and root modules flatten", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, "tests/.keep")
		for _, p := range []string{"src/parser.py", "parser.py"} {
			got, err := a.TestPath(root, Target{Name: "parse_line", Path: p, Language: "python"})
			if err != nil || got != "tests/test_parser.py" {
				t.Fatalf("Path %s: got %q, %v", p, got, err)
			}
		}
	})

	t.Run("conventional even when file exists", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, "pkg/test_parser.py")
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "pkg/test_parser.py" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestPythonAdapterRunCommand(t *testing.T) {
	a := &PythonAdapter{}
	tgt := Target{Name: "parse_line", Path: "pkg/parser.py", Language: "python"}
	argv, err := a.RunCommand(t.TempDir(), "pkg/test_parser.py", tgt)
	if err != nil {
		t.Fatal(err)
	}
	want := "python3 -m pytest pkg/test_parser.py -k parse_line_gen -q"
	if strings.Join(argv, " ") != want {
		t.Fatalf("RunCommand = %v, want %q", argv, want)
	}
}

func TestPythonAdapterPromptFragment(t *testing.T) {
	a := &PythonAdapter{}
	tgt := Target{Name: "Store.Save", Path: "pkg/store.py", Language: "python"}
	frag := a.PromptFragment(tgt, "pkg/test_store.py")
	for _, want := range []string{"test_store_save_gen", "pkg/test_store.py", "pytest", "tu-agent:gen:start"} {
		if !strings.Contains(frag, want) {
			t.Errorf("PromptFragment missing %q:\n%s", want, frag)
		}
	}
}

func TestPythonPromptFragment_coverage(t *testing.T) {
	a := &PythonAdapter{}
	frag := a.PromptFragment(Target{Name: "foo", Path: "pkg/foo.py", Language: "python"}, "pkg/test_foo.py")
	for _, want := range []string{"branches", "monkeypatch"} {
		if !strings.Contains(frag, want) {
			t.Errorf("Python prompt missing coverage guidance %q", want)
		}
	}
}
