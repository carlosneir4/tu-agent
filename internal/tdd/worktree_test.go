package tdd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPartitionTests(t *testing.T) {
	paths := []string{
		"core/src/test/java/com/acme/FooTest.java",
		"core/src/main/java/com/acme/Foo.java",
		"internal/tdd/redgate.go",
		"internal/tdd/redgate_test.go",
	}
	tests, prod := PartitionTests(paths)
	wantTests := []string{
		"core/src/test/java/com/acme/FooTest.java",
		"internal/tdd/redgate_test.go",
	}
	wantProd := []string{
		"core/src/main/java/com/acme/Foo.java",
		"internal/tdd/redgate.go",
	}
	if !reflect.DeepEqual(tests, wantTests) {
		t.Errorf("tests = %v, want %v", tests, wantTests)
	}
	if !reflect.DeepEqual(prod, wantProd) {
		t.Errorf("prod = %v, want %v", prod, wantProd)
	}
}

func TestPartitionTestsRootModule(t *testing.T) {
	// Regression test: root-level Maven/Gradle modules without directory prefix.
	paths := []string{
		"src/test/java/com/acme/FooTest.java",
		"src/main/java/com/acme/Foo.java",
	}
	tests, prod := PartitionTests(paths)
	wantTests := []string{
		"src/test/java/com/acme/FooTest.java",
	}
	wantProd := []string{
		"src/main/java/com/acme/Foo.java",
	}
	if !reflect.DeepEqual(tests, wantTests) {
		t.Errorf("tests = %v, want %v", tests, wantTests)
	}
	if !reflect.DeepEqual(prod, wantProd) {
		t.Errorf("prod = %v, want %v", prod, wantProd)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestSnapshotDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	// Baseline commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(root, "seed"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-qm", "seed")

	ctx := context.Background()
	before, err := Snapshot(ctx, root)
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new_test.go"), []byte("package p"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Snapshot(ctx, root)
	if err != nil {
		t.Fatalf("snapshot after: %v", err)
	}
	files, err := DiffFiles(ctx, root, before, after)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(files) != 1 || files[0] != "new_test.go" {
		t.Fatalf("diff = %v, want [new_test.go]", files)
	}
}

func TestSnapshotPreservesStagedIndex(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	// Baseline commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(root, "seed"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "seed")

	// Stage a new file so the real index differs from HEAD before Snapshot runs.
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "staged.txt")

	statusBefore := run("status", "--porcelain")

	ctx := context.Background()
	if _, err := Snapshot(ctx, root); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	statusAfter := run("status", "--porcelain")
	if statusAfter != statusBefore {
		t.Fatalf("Snapshot mutated the real index: before=%q after=%q", statusBefore, statusAfter)
	}
	if !strings.Contains(statusAfter, "A  staged.txt") {
		t.Fatalf("staged.txt no longer staged after Snapshot: status=%q", statusAfter)
	}
}
