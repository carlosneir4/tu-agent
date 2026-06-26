package codegen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestDetectBuildTool_Maven(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "maven" {
		t.Errorf("got %q, want maven", got)
	}
}

func TestDetectBuildTool_Gradle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "gradle" {
		t.Errorf("got %q, want gradle", got)
	}
}

func TestDetectBuildTool_GradleKts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte("plugins {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "gradle" {
		t.Errorf("got %q, want gradle", got)
	}
}

func TestDetectBuildTool_NPM(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "npm" {
		t.Errorf("got %q, want npm", got)
	}
}

func TestDetectBuildTool_Yarn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "yarn" {
		t.Errorf("got %q, want yarn", got)
	}
}

func TestDetectBuildTool_PNPM(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "pnpm" {
		t.Errorf("got %q, want pnpm", got)
	}
}

func TestDetectBuildTool_BunLockb(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "bun" {
		t.Errorf("got %q, want bun", got)
	}
}

func TestDetectBuildTool_BunTextLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bun.lock"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "bun" {
		t.Errorf("got %q, want bun", got)
	}
}

func TestDetectBuildTool_BunPackageManagerField(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"packageManager": "bun@1.1.30"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "bun" {
		t.Errorf("got %q, want bun (from packageManager field)", got)
	}
}

func TestDetectBuildTool_BunBeatsNpm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "bun" {
		t.Errorf("expected bun lockfile to win, got %q", got)
	}
}

func TestDetectBuildTool_PyProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[build-system]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "pyproject" {
		t.Errorf("got %q, want pyproject", got)
	}
}

func TestDetectBuildTool_Pip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "pip" {
		t.Errorf("got %q, want pip", got)
	}
}

func TestDetectBuildTool_Make(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "make" {
		t.Errorf("got %q, want make", got)
	}
}

func TestDetectBuildTool_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "go" {
		t.Errorf("got %q, want go", got)
	}
}

func TestDetectBuildTool_Unknown(t *testing.T) {
	dir := t.TempDir()
	if got := codegen.DetectBuildTool(dir); got != "unknown" {
		t.Errorf("got %q, want unknown", got)
	}
}

func TestDetectBuildTool_MavenBeatsGradle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "maven" {
		t.Errorf("expected maven precedence over gradle, got %q", got)
	}
}

func TestDetectBuildTool_GradlewBeatsPom(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "gradle" {
		t.Errorf("expected gradlew wrapper to win over pom.xml, got %q", got)
	}
}

func TestDetectBuildTool_MvnwBeatsGradle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectBuildTool(dir); got != "maven" {
		t.Errorf("expected mvnw wrapper to win over build.gradle, got %q", got)
	}
}

func TestDetectTestCommand(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"maven", "mvn test"},
		{"gradle", "./gradlew test"},
		{"npm", "npm test"},
		{"yarn", "yarn test"},
		{"pnpm", "pnpm test"},
		{"bun", "bun test"},
		{"pyproject", "pytest"},
		{"pip", "pytest"},
		{"make", "make test"},
		{"go", "go test ./..."},
		{"unknown", ""},
	}
	for _, tc := range cases {
		if got := codegen.DetectTestCommand(tc.tool); got != tc.want {
			t.Errorf("DetectTestCommand(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

func TestDetectTestCommandForRoot_BunMonorepo(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"scripts": {"test": "turbo run test"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectTestCommandForRoot(dir); got != "bun test" {
		t.Errorf("got %q, want bun test", got)
	}
}

func TestDetectTestCommandForRoot_NpmWithTestScript(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"scripts": {"test": "jest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectTestCommandForRoot(dir); got != "npm test" {
		t.Errorf("got %q, want npm test", got)
	}
}

func TestDetectTestCommandForRoot_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectTestCommandForRoot(dir); got != "go test ./..." {
		t.Errorf("got %q, want go test ./...", got)
	}
}

func TestDetectTestCommandForRoot_Maven(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegen.DetectTestCommandForRoot(dir); got != "mvn test" {
		t.Errorf("got %q, want mvn test", got)
	}
}

func TestDetectTestCommandForRoot_Unknown(t *testing.T) {
	dir := t.TempDir()
	if got := codegen.DetectTestCommandForRoot(dir); got != "" {
		t.Errorf("got %q, want empty for unknown project", got)
	}
}

func TestDetectPrimaryLanguage_Java(t *testing.T) {
	paths := []string{"src/Foo.java", "src/Bar.java", "README.md"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "java" {
		t.Errorf("got %q, want java", got)
	}
}

func TestDetectPrimaryLanguage_TypeScript(t *testing.T) {
	paths := []string{"src/index.ts", "src/app.tsx", "src/utils.ts"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "typescript" {
		t.Errorf("got %q, want typescript", got)
	}
}

func TestDetectPrimaryLanguage_Python(t *testing.T) {
	paths := []string{"app.py", "models/user.py", "tests/test_app.py"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "python" {
		t.Errorf("got %q, want python", got)
	}
}

func TestDetectPrimaryLanguage_Go(t *testing.T) {
	paths := []string{"main.go", "handler/api.go"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "go" {
		t.Errorf("got %q, want go", got)
	}
}

func TestDetectPrimaryLanguage_Unknown(t *testing.T) {
	paths := []string{"README.md", "config.yaml"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "unknown" {
		t.Errorf("got %q, want unknown", got)
	}
}

func TestDetectPrimaryLanguage_PicksHighest(t *testing.T) {
	paths := []string{"Foo.java", "Bar.java", "a.py", "b.py", "c.py"}
	if got := codegen.DetectPrimaryLanguage(paths); got != "python" {
		t.Errorf("got %q, want python (most files)", got)
	}
}
