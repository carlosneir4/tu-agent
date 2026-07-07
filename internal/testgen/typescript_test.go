package testgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePkgJSON(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTSAdapterDetect(t *testing.T) {
	a := &TSAdapter{}
	root := t.TempDir()
	err := a.Detect(root)
	if err == nil {
		t.Fatal("Detect on empty dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--dry-run") {
		t.Fatalf("Detect error must point at --dry-run, got: %v", err)
	}

	writePkgJSON(t, root, `{"name":"x","devDependencies":{"vitest":"^3.0.0"}}`)
	if err := a.Detect(root); err != nil {
		t.Fatalf("Detect with vitest devDependency: %v", err)
	}

	root2 := t.TempDir()
	writePkgJSON(t, root2, `{"name":"x","dependencies":{"jest":"^29.0.0"}}`)
	if err := (&TSAdapter{}).Detect(root2); err != nil {
		t.Fatalf("Detect with jest dependency: %v", err)
	}
}

func TestTSAdapterTestPath(t *testing.T) {
	a := &TSAdapter{}

	t.Run("sibling .test.ts", func(t *testing.T) {
		root := t.TempDir()
		tgt := Target{Name: "slugify", Path: "src/slug.ts", Language: "typescript"}
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "src/slug.test.ts" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("tsx source mirrors extension", func(t *testing.T) {
		root := t.TempDir()
		tgt := Target{Name: "Button", Path: "src/ui/Button.tsx", Language: "typescript"}
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "src/ui/Button.test.tsx" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("conventional even when file exists", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, "src/slug.test.ts")
		tgt := Target{Name: "slugify", Path: "src/slug.ts", Language: "typescript"}
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "src/slug.test.ts" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("js source keeps .js extension", func(t *testing.T) {
		root := t.TempDir()
		tgt := Target{Name: "add", Path: "src/a.js", Language: "typescript"}
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "src/a.test.js" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("jsx source keeps .jsx extension", func(t *testing.T) {
		root := t.TempDir()
		tgt := Target{Name: "Widget", Path: "src/b.jsx", Language: "typescript"}
		got, err := a.TestPath(root, tgt)
		if err != nil || got != "src/b.test.jsx" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestTSAdapterRunCommand(t *testing.T) {
	tgt := Target{Name: "slugify", Path: "src/slug.ts", Language: "typescript"}

	t.Run("vitest", func(t *testing.T) {
		root := t.TempDir()
		writePkgJSON(t, root, `{"devDependencies":{"vitest":"^3.0.0"}}`)
		argv, err := (&TSAdapter{}).RunCommand(root, "src/slug.test.ts", tgt)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(argv, " "); got != `npx vitest run src/slug.test.ts -t slugify.*\(gen\)` {
			t.Fatalf("RunCommand = %q", got)
		}
	})

	t.Run("jest", func(t *testing.T) {
		root := t.TempDir()
		writePkgJSON(t, root, `{"devDependencies":{"jest":"^29.0.0"}}`)
		argv, err := (&TSAdapter{}).RunCommand(root, "src/slug.test.ts", tgt)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(argv, " "); got != `npx jest src/slug.test.ts -t slugify.*\(gen\)` {
			t.Fatalf("RunCommand = %q", got)
		}
	})

	t.Run("both present, no test files, ties to vitest by convention", func(t *testing.T) {
		root := t.TempDir()
		writePkgJSON(t, root, `{"devDependencies":{"jest":"^29.0.0","vitest":"^3.0.0"}}`)
		argv, err := (&TSAdapter{}).RunCommand(root, "src/slug.test.ts", tgt)
		if err != nil || argv[1] != "vitest" {
			t.Fatalf("RunCommand = %v, %v — want vitest", argv, err)
		}
	})

	t.Run("no runner returns vitest default, no error", func(t *testing.T) {
		argv, err := (&TSAdapter{}).RunCommand(t.TempDir(), "src/slug.test.ts", tgt)
		if err != nil {
			t.Fatalf("want nil error on missing runner, got: %v", err)
		}
		if len(argv) < 2 || argv[1] != "vitest" {
			t.Fatalf("want vitest default command, got: %v", argv)
		}
	})
}

func TestTSAdapterRunCommand_packageManager(t *testing.T) {
	// The exec prefix must follow the repo's package manager, not a hardcoded
	// npx — in a bun or pnpm repo `npx vitest` is non-idiomatic and can fail.
	tgt := Target{Name: "slugify", Path: "src/slug.ts", Language: "typescript"}
	cases := []struct{ name, lock, want string }{
		{"bun", "bun.lockb", `bunx vitest run src/slug.test.ts -t slugify.*\(gen\)`},
		{"pnpm", "pnpm-lock.yaml", `pnpm exec vitest run src/slug.test.ts -t slugify.*\(gen\)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writePkgJSON(t, root, `{"devDependencies":{"vitest":"^3.0.0"}}`)
			writeFiles(t, root, c.lock)
			argv, err := (&TSAdapter{}).RunCommand(root, "src/slug.test.ts", tgt)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(argv, " "); got != c.want {
				t.Fatalf("RunCommand = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTSResolve_app_inheritsRootJestByConvention(t *testing.T) {
	a := &TSAdapter{}
	tgt := Target{Name: "widget", Path: "packages/app/src/widget.ts", Language: "typescript"}
	r, err := a.resolve("testdata/ws", tgt)
	if err != nil {
		t.Fatal(err)
	}
	// app declares neither; root declares BOTH -> tie broken by app's .spec
	// majority -> jest. Suffix follows the same .spec majority.
	if r.pkgDir != "packages/app" || r.framework != "jest" || r.suffix != ".spec" || !r.declared {
		t.Fatalf("resolve(app) = %+v, want pkgDir=packages/app framework=jest suffix=.spec declared=true", r)
	}
}

func TestTSResolve_web_declaresVitest(t *testing.T) {
	a := &TSAdapter{}
	tgt := Target{Name: "card", Path: "packages/web/src/card.ts", Language: "typescript"}
	r, err := a.resolve("testdata/ws", tgt)
	if err != nil {
		t.Fatal(err)
	}
	if r.pkgDir != "packages/web" || r.framework != "vitest" || r.suffix != ".test" || !r.declared {
		t.Fatalf("resolve(web) = %+v, want pkgDir=packages/web framework=vitest suffix=.test declared=true", r)
	}
}

func TestTSTestPath_usesResolvedSuffix(t *testing.T) {
	a := &TSAdapter{}
	got, err := a.TestPath("testdata/ws", Target{Name: "widget", Path: "packages/app/src/widget.ts", Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "packages/app/src/widget.spec.ts" {
		t.Errorf("TestPath = %q, want packages/app/src/widget.spec.ts", got)
	}
}

func TestTSRunCommand_packageRelativeAndFramework(t *testing.T) {
	a := &TSAdapter{}
	tgt := Target{Name: "widget", Path: "packages/app/src/widget.ts", Language: "typescript"}
	argv, err := a.RunCommand("testdata/ws", "packages/app/src/widget.spec.ts", tgt)
	if err != nil {
		t.Fatal(err)
	}
	// jest, package-relative path (no "packages/app/" prefix), no "run" verb.
	want := []string{"npx", "jest", "src/widget.spec.ts", "-t", tsGenRunPattern(tgt)}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestTSRunCommand_vitestKeepsRunVerb(t *testing.T) {
	a := &TSAdapter{}
	tgt := Target{Name: "card", Path: "packages/web/src/card.ts", Language: "typescript"}
	argv, err := a.RunCommand("testdata/ws", "packages/web/src/card.test.ts", tgt)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"npx", "vitest", "run", "src/card.test.ts", "-t", tsGenRunPattern(tgt)}
	for i := range want {
		if i >= len(argv) || argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestTSDetect_rootPackageJSON(t *testing.T) {
	a := &TSAdapter{}
	if err := a.Detect("testdata/ws"); err != nil {
		t.Errorf("Detect(ws) = %v, want nil (root package.json exists)", err)
	}
	if err := a.Detect("testdata"); err == nil {
		t.Error("Detect(testdata) = nil, want error (no package.json)")
	}
}

func TestTSAdapterPromptFragment(t *testing.T) {
	tgt := Target{Name: "slugify", Path: "src/slug.ts", Language: "typescript"}

	t.Run("names cached framework", func(t *testing.T) {
		root := t.TempDir()
		writePkgJSON(t, root, `{"devDependencies":{"jest":"^29.0.0"}}`)
		a := &TSAdapter{}
		if _, err := a.RunCommand(root, "src/slug.test.ts", tgt); err != nil {
			t.Fatal(err)
		}
		frag := a.PromptFragment(tgt, "src/slug.test.ts")
		for _, want := range []string{"jest", "src/slug.test.ts", "describe", "tu-agent:gen:start"} {
			if !strings.Contains(frag, want) {
				t.Errorf("PromptFragment missing %q:\n%s", want, frag)
			}
		}
	})

	t.Run("defaults to vitest when undetected", func(t *testing.T) {
		frag := (&TSAdapter{}).PromptFragment(tgt, "src/slug.test.ts")
		if !strings.Contains(frag, "vitest") {
			t.Errorf("PromptFragment missing vitest fallback:\n%s", frag)
		}
	})
}

func TestTSPromptFragment_coverage(t *testing.T) {
	a := &TSAdapter{}
	frag := a.PromptFragment(Target{Name: "foo", Path: "src/foo.ts", Language: "typescript"}, "src/foo.test.ts")
	for _, want := range []string{"branches", "vi.mock", "jest.mock"} {
		if !strings.Contains(frag, want) {
			t.Errorf("TS prompt missing coverage guidance %q", want)
		}
	}
}
