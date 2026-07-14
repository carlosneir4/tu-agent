package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateGoParsesProfile(t *testing.T) {
	root := t.TempDir()
	// Fake runner: writes a coverprofile where the real `go test` would.
	run := func(repoRoot string, argv []string) (string, error) {
		out := ""
		for _, a := range argv {
			if rest, ok := cutPrefix(a, "-coverprofile="); ok {
				out = rest
			}
		}
		if out == "" {
			return "", fmt.Errorf("no -coverprofile flag")
		}
		prof := "mode: set\n" + "github.com/carlosneir4/tu-agent/a.go:1.1,3.2 2 1\n"
		return "", os.WriteFile(out, []byte(prof), 0o644)
	}
	p, err := Generate(LangGo, root, "github.com/carlosneir4/tu-agent", run)
	if err != nil {
		t.Fatal(err)
	}
	if ratio, ok := p.SymbolCoverage("a.go", 1, 3); !ok || ratio != 1.0 {
		t.Fatalf("ratio=%v ok=%v want 1.0,true", ratio, ok)
	}
}

func TestExecRunnerEmptyArgv(t *testing.T) {
	if _, err := ExecRunner(".", nil); err == nil {
		t.Fatal("ExecRunner(nil argv): want error, got nil")
	}
}

func TestGenerateTS_runsInPackageAndParses(t *testing.T) {
	repo := t.TempDir()
	pkg := filepath.Join(repo, "packages", "app")
	if err := os.MkdirAll(filepath.Join(pkg, "coverage"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Istanbul coverage-final.json: one file, line 1 covered, line 2 not.
	report := `{"packages/app/src/widget.ts":{"path":"` + filepath.Join(pkg, "src", "widget.ts") +
		`","statementMap":{"0":{"start":{"line":1},"end":{"line":1}},"1":{"start":{"line":2},"end":{"line":2}}},"s":{"0":1,"1":0}}}`
	if err := os.WriteFile(filepath.Join(pkg, "coverage", "coverage-final.json"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotDir string
	var gotArgv []string
	run := func(dir string, argv []string) (string, error) {
		gotDir, gotArgv = dir, argv
		return "ok", nil
	}
	prof, err := GenerateTS(repo, "packages/app", "jest", run)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != filepath.Join(repo, "packages/app") {
		t.Errorf("run cwd = %q, want the package dir", gotDir)
	}
	if len(gotArgv) < 3 || gotArgv[1] != "jest" || gotArgv[len(gotArgv)-1] != "--coverage" {
		t.Errorf("argv = %v, want npx jest … --coverage", gotArgv)
	}
	ratio, ok := prof.SymbolCoverage("packages/app/src/widget.ts", 1, 2)
	if !ok || ratio != 0.5 {
		t.Errorf("SymbolCoverage = %v,%v, want 0.5,true", ratio, ok)
	}
}

func TestProfileMerge_coveredWins(t *testing.T) {
	a := Profile{}
	a.add("f.ts", 1, true)
	a.add("f.ts", 2, false)
	b := Profile{}
	b.add("f.ts", 2, true)
	b.add("g.ts", 1, true)
	a.Merge(b)
	if r, ok := a.SymbolCoverage("f.ts", 2, 2); !ok || r != 1 {
		t.Errorf("f.ts:2 merged = %v,%v, want 1,true (covered wins)", r, ok)
	}
	if r, ok := a.SymbolCoverage("g.ts", 1, 1); !ok || r != 1 {
		t.Errorf("g.ts:1 = %v,%v, want 1,true", r, ok)
	}
	// reverse: a covered, b uncovered — a must stay covered (covered-wins both ways)
	c := Profile{}
	c.add("h.ts", 5, true)
	d := Profile{}
	d.add("h.ts", 5, false)
	c.Merge(d)
	if r, ok := c.SymbolCoverage("h.ts", 5, 5); !ok || r != 1 {
		t.Errorf("h.ts:5 should stay covered after merge with uncovered: %v,%v", r, ok)
	}
}

// cutPrefix is strings.CutPrefix (Go 1.20+); inlined to avoid an import churn.
func cutPrefix(s, p string) (string, bool) {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return "", false
}
