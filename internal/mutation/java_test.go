package mutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pitFixture = `<?xml version="1.0" encoding="UTF-8"?>
<mutations>
  <mutation detected='true' status='KILLED'><sourceFile>Order.java</sourceFile><lineNumber>10</lineNumber><mutator>MathMutator</mutator></mutation>
  <mutation detected='true' status='KILLED'><sourceFile>Order.java</sourceFile><lineNumber>12</lineNumber><mutator>NegateConditionals</mutator></mutation>
  <mutation detected='false' status='SURVIVED'><sourceFile>Order.java</sourceFile><lineNumber>20</lineNumber><mutator>VoidMethodCalls</mutator></mutation>
</mutations>`

func TestJavaEngineParse(t *testing.T) {
	rep, err := javaEngine{}.Parse(pitFixture)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 3 || rep.Killed != 2 || rep.Survived != 1 {
		t.Fatalf("counts = %+v, want total3 killed2 survived1", rep)
	}
	if rep.Score < 0.66 || rep.Score > 0.67 {
		t.Errorf("score = %v, want ~0.667", rep.Score)
	}
	if len(rep.Survivors) != 1 || rep.Survivors[0].Line != 20 || rep.Survivors[0].File != "Order.java" {
		t.Fatalf("survivor = %+v", rep.Survivors)
	}
}

func TestJavaBuildTool(t *testing.T) {
	mvn := t.TempDir()
	if err := os.WriteFile(filepath.Join(mvn, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := javaBuildTool(mvn); got != "maven" {
		t.Errorf("pom.xml → %q, want maven", got)
	}
	gr := t.TempDir()
	if err := os.WriteFile(filepath.Join(gr, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := javaBuildTool(gr); got != "gradle" {
		t.Errorf("build.gradle → %q, want gradle", got)
	}
	both := t.TempDir()
	if err := os.WriteFile(filepath.Join(both, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(both, "build.gradle.kts"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := javaBuildTool(both); got != "gradle" {
		t.Errorf("both → %q, want gradle (gradle wins)", got)
	}
}

func TestJavaCommand_maven(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(javaEngine{}.Command(repo, ""), " ")
	for _, want := range []string{"mvn", "org.pitest:pitest-maven:mutationCoverage", "-DoutputFormats=XML", "-DtimestampedReports=false"} {
		if !strings.Contains(joined, want) {
			t.Errorf("maven argv %q missing %q", joined, want)
		}
	}
}

func TestJavaCommand_gradle(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	argv := javaEngine{}.Command(repo, "")
	if len(argv) != 2 || argv[0] != "gradle" || argv[1] != "pitest" {
		t.Errorf("gradle argv = %v, want [gradle pitest]", argv)
	}
}

func TestJavaCommand_gradleWrapper(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gradlew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	argv := javaEngine{}.Command(repo, "")
	if argv[0] != "./gradlew" {
		t.Errorf("argv[0] = %q, want ./gradlew", argv[0])
	}
}

func TestJavaModuleDir_multiModuleGradle(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(repo, "core")
	if err := os.MkdirAll(filepath.Join(mod, "src", "main", "java", "com", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join("core", "src", "main", "java", "com", "acme")
	if got := javaModuleDir(repo, pkgDir); got != "core" {
		t.Fatalf("javaModuleDir = %q, want core", got)
	}
	argv := javaEngine{}.Command(repo, pkgDir)
	if argv[len(argv)-1] != ":core:pitest" {
		t.Errorf("gradle multi-module task = %v, want :core:pitest", argv)
	}
	if got := (javaEngine{}).ReportPath(repo, pkgDir); got != filepath.Join(repo, "core", "build", "reports", "pitest", "mutations.xml") {
		t.Errorf("multi-module ReportPath = %q", got)
	}
}

func TestJavaModuleDir_singleModuleRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := javaModuleDir(repo, "src/main/java/com/acme"); got != "." {
		t.Fatalf("single-module javaModuleDir = %q, want .", got)
	}
	if got := gradleTask("."); got != "pitest" {
		t.Errorf("gradleTask(.) = %q, want pitest", got)
	}
}

func TestJavaCommand_mavenMultiModule(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(repo, "core")
	if err := os.MkdirAll(filepath.Join(mod, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(javaEngine{}.Command(repo, filepath.Join("core", "src")), " ")
	if !strings.Contains(joined, "-pl core") {
		t.Errorf("maven multi-module argv %q missing -pl core", joined)
	}
}

func TestJavaReportPath(t *testing.T) {
	mvn := t.TempDir()
	if err := os.WriteFile(filepath.Join(mvn, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := (javaEngine{}).ReportPath(mvn, "")
	want := filepath.Join(mvn, "target", "pit-reports", "mutations.xml")
	if got != want {
		t.Errorf("maven ReportPath = %q, want %q", got, want)
	}
	gr := t.TempDir()
	if err := os.WriteFile(filepath.Join(gr, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got = (javaEngine{}).ReportPath(gr, "")
	want = filepath.Join(gr, "build", "reports", "pitest", "mutations.xml")
	if got != want {
		t.Errorf("gradle ReportPath = %q, want %q", got, want)
	}
}

func TestJavaModuleDir_absoluteGuard(t *testing.T) {
	// An absolute pkgDir must not loop; it degrades to ".".
	if got := javaModuleDir(t.TempDir(), "/abs/src/main/java/com/acme"); got != "." {
		t.Fatalf("javaModuleDir(abs) = %q, want . (graceful degrade, no hang)", got)
	}
}
