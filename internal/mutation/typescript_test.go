package mutation

import (
	"os"
	"path/filepath"
	"testing"
)

const strykerFixture = `{
  "files": {
    "src/order.ts": {
      "mutants": [
        {"id":"1","mutatorName":"BooleanLiteral","status":"Killed","location":{"start":{"line":3}}},
        {"id":"2","mutatorName":"ArithmeticOperator","status":"Timeout","location":{"start":{"line":4}}},
        {"id":"3","mutatorName":"ConditionalExpression","status":"Survived","location":{"start":{"line":5}}}
      ]
    }
  }
}`

func TestTSEngine_WorkDirAndAvailable(t *testing.T) {
	repo := t.TempDir()
	pkg := filepath.Join(repo, "packages", "app")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also create packages/app/src so the walk-up cases have a real directory.
	if err := os.MkdirAll(filepath.Join(pkg, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := tsEngine{}

	// Direct match: package.json lives in packages/app.
	if !e.Available(repo, "packages/app") {
		t.Error("Available should be true: packages/app has package.json")
	}
	if got := e.WorkDir(repo, "packages/app"); got != filepath.Join(repo, "packages/app") {
		t.Errorf("WorkDir = %q, want the package dir", got)
	}

	// Walk-up: pkgDir is packages/app/src but package.json is in packages/app.
	if !e.Available(repo, "packages/app/src") {
		t.Error("Available should be true: walk-up from packages/app/src finds packages/app/package.json")
	}
	if got, want := e.WorkDir(repo, "packages/app/src"), filepath.Join(repo, "packages/app"); got != want {
		t.Errorf("WorkDir walk-up = %q, want %q", got, want)
	}

	// Missing: no package.json anywhere on the chain below repo root.
	if e.Available(repo, "packages/missing") {
		t.Error("Available should be false: packages/missing has no package.json")
	}

	// Single-package: package.json at repo root, pkgDir is "src".
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !e.Available(repo, "src") {
		t.Error("Available should be true: walk-up from src finds root package.json")
	}
	if got := e.WorkDir(repo, "src"); got != repo {
		t.Errorf("WorkDir single-package = %q, want repo root %q", got, repo)
	}
}

func TestTSEngineParse(t *testing.T) {
	rep, err := tsEngine{}.Parse(strykerFixture)
	if err != nil {
		t.Fatal(err)
	}
	// Killed + Timeout count as killed → 2 killed, 1 survived, total 3
	if rep.Total != 3 || rep.Killed != 2 || rep.Survived != 1 {
		t.Fatalf("counts = %+v, want total3 killed2 survived1", rep)
	}
	if len(rep.Survivors) != 1 || rep.Survivors[0].Line != 5 || rep.Survivors[0].File != "src/order.ts" {
		t.Fatalf("survivor = %+v", rep.Survivors)
	}
}
