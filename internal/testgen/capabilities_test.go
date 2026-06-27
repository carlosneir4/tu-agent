package testgen

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectCapabilities_java_mockitoInline(t *testing.T) {
	root := t.TempDir()
	// The mockito-inline mock-maker registration enables mockStatic.
	writeFiles(t, root, filepath.Join("src", "test", "resources", "mockito-extensions", "org.mockito.plugins.MockMaker"))
	got := DetectCapabilities(root, "java")
	want := []string{"mockStatic available (mockito-inline mock-maker registered)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectCapabilities(java) = %v, want %v", got, want)
	}
}

func TestDetectCapabilities_python_pytestMock(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "requirements.txt")
	if err := writeFileContent(filepath.Join(root, "requirements.txt"), "pytest\npytest-mock>=3\n"); err != nil {
		t.Fatal(err)
	}
	got := DetectCapabilities(root, "python")
	want := []string{"pytest-mock available (use the mocker fixture)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectCapabilities(python) = %v, want %v", got, want)
	}
}

func TestDetectCapabilities_none(t *testing.T) {
	root := t.TempDir()
	if got := DetectCapabilities(root, "java"); len(got) != 0 {
		t.Fatalf("DetectCapabilities(empty java) = %v, want none", got)
	}
}
