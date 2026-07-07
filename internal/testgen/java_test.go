package testgen

import (
	"strings"
	"testing"
)

func TestJavaAdapterDetect(t *testing.T) {
	a := &JavaAdapter{}
	tests := []struct {
		name    string
		files   []string
		wantErr bool
	}{
		{"empty", nil, true},
		{"maven", []string{"pom.xml"}, false},
		{"gradle", []string{"build.gradle"}, false},
		{"gradle kts", []string{"build.gradle.kts"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tt.files...)
			err := a.Detect(root)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Detect err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJavaAdapterTestPath(t *testing.T) {
	a := &JavaAdapter{}
	tgt := Target{Name: "Foo.bar", Path: "src/main/java/com/acme/Foo.java", Language: "java"}

	root := t.TempDir()
	got, err := a.TestPath(root, tgt)
	if err != nil || got != "src/test/java/com/acme/FooTest.java" {
		t.Fatalf("free path: got %q, %v", got, err)
	}

	writeFiles(t, root, "src/test/java/com/acme/FooTest.java")
	got, err = a.TestPath(root, tgt) // still conventional even when it exists
	if err != nil || got != "src/test/java/com/acme/FooTest.java" {
		t.Fatalf("existing: got %q, %v", got, err)
	}

	// Unconventional layout: no src/main/java → explicit error.
	bad := Target{Name: "X.y", Path: "lib/X.java", Language: "java"}
	if _, err := a.TestPath(root, bad); err == nil {
		t.Fatal("unconventional layout: want error, got nil")
	}
}

func TestJavaAdapterRunCommand(t *testing.T) {
	a := &JavaAdapter{}
	tgt := Target{Name: "Foo.bar", Path: "src/main/java/com/acme/Foo.java", Language: "java"}
	testPath := "src/test/java/com/acme/FooTest.java"

	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{"maven", []string{"pom.xml"}, "mvn -q test -Dtest=FooTest#barGen* -DfailIfNoTests=false"},
		{"maven wrapper", []string{"pom.xml", "mvnw"}, "./mvnw -q test -Dtest=FooTest#barGen* -DfailIfNoTests=false"},
		{"gradle", []string{"build.gradle"}, "gradle test --tests com.acme.FooTest.barGen*"},
		{"gradle wrapper", []string{"build.gradle.kts", "gradlew"}, "./gradlew test --tests com.acme.FooTest.barGen*"},
		// Dual-build repos carry both a pom.xml and a build.gradle.
		// A committed wrapper is the strongest signal of the team's real tool and
		// must win over a bare build file of the other ecosystem.
		{"dual build: gradlew wins over pom.xml", []string{"pom.xml", "build.gradle", "gradlew"}, "./gradlew test --tests com.acme.FooTest.barGen*"},
		{"dual build: mvnw wins over build.gradle", []string{"build.gradle", "mvnw"}, "./mvnw -q test -Dtest=FooTest#barGen* -DfailIfNoTests=false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tt.files...)
			argv, err := a.RunCommand(root, testPath, tgt)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(argv, " "); got != tt.want {
				t.Fatalf("RunCommand = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := a.RunCommand(t.TempDir(), testPath, tgt); err == nil {
		t.Fatal("no runner: want error, got nil")
	}
}

// TestJavaAdapterRunCommandMultiModule pins the Gradle FQCN derivation AND the
// module-scoped task for a multi-module layout where the test path is
// prefixed by the module directory (e.g. core/src/test/java/...). The
// package — not the module path — must form the --tests filter, and the
// task itself must be scoped to the module (:core:test) so the build does not
// run every module's tests from the root.
func TestJavaAdapterRunCommandMultiModule(t *testing.T) {
	a := &JavaAdapter{}
	tgt := Target{Name: "IngestionResult.error", Path: "core/src/main/java/com/acme/ingest/IngestionResult.java", Language: "java"}
	testPath := "core/src/test/java/com/acme/ingest/IngestionResultTest.java"

	root := t.TempDir()
	writeFiles(t, root, "settings.gradle", "gradlew", "core/build.gradle")
	argv, err := a.RunCommand(root, testPath, tgt)
	if err != nil {
		t.Fatal(err)
	}
	want := "./gradlew :core:test --tests com.acme.ingest.IngestionResultTest.errorGen*"
	if got := strings.Join(argv, " "); got != want {
		t.Fatalf("RunCommand = %q, want %q", got, want)
	}
}

// TestJavaAdapterRunCommandMavenMultiModule mirrors the Gradle case for Maven:
// the test path lives under a module (core/...) that owns its own pom.xml, so
// the run command must be scoped with -pl and must not hard-fail the whole
// reactor build when the module's -Dtest filter matches nothing.
func TestJavaAdapterRunCommandMavenMultiModule(t *testing.T) {
	a := &JavaAdapter{}
	tgt := Target{Name: "Foo.bar", Path: "core/src/main/java/com/acme/Foo.java", Language: "java"}
	testPath := "core/src/test/java/com/acme/FooTest.java"

	root := t.TempDir()
	writeFiles(t, root, "pom.xml", "core/pom.xml")
	argv, err := a.RunCommand(root, testPath, tgt)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{"-pl core", "-DfailIfNoTests=false", "-Dtest=FooTest#barGen*"} {
		if !strings.Contains(joined, want) {
			t.Errorf("maven multi-module RunCommand = %q, missing %q", joined, want)
		}
	}
}

func TestJavaAdapterPromptFragment(t *testing.T) {
	a := &JavaAdapter{}
	tgt := Target{Name: "Foo.bar", Path: "src/main/java/com/acme/Foo.java", Language: "java"}
	frag := a.PromptFragment(tgt, "src/test/java/com/acme/FooTest.java")
	for _, want := range []string{"FooTest", "JUnit 5", "org.junit.jupiter", "barGen", "tu-agent:gen:start", "NO underscores"} {
		if !strings.Contains(frag, want) {
			t.Errorf("PromptFragment missing %q:\n%s", want, frag)
		}
	}
	for _, want := range []string{"branches", "spies", "mockStatic"} {
		if !strings.Contains(frag, want) {
			t.Errorf("Java prompt should encourage real coverage; missing %q", want)
		}
	}
	if strings.Contains(frag, "only when the context shows they cannot be constructed") {
		t.Errorf("Java prompt still carries the anti-mock bias")
	}
}
