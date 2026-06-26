package testgen

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

func TestTargetFromNodeAndPrefix(t *testing.T) {
	n := graph.Node{
		ID: "internal/store/store.go::Store.Save", Name: "Store.Save",
		Path: "internal/store/store.go", Line: 10, EndLine: 30,
		Language: "go", Params: "(v string)", ReturnType: "error",
	}
	tgt := TargetFromNode(n)
	if tgt.NodeID != n.ID || tgt.Name != "Store.Save" || tgt.Path != n.Path ||
		tgt.Line != 10 || tgt.EndLine != 30 || tgt.Language != "go" {
		t.Fatalf("TargetFromNode = %+v", tgt)
	}
	if got := tgt.TestFuncPrefix(); got != "TestStoreSave" {
		t.Fatalf("TestFuncPrefix = %q, want TestStoreSave", got)
	}
	plain := Target{Name: "ParseGo"}
	if got := plain.TestFuncPrefix(); got != "TestParseGo" {
		t.Fatalf("TestFuncPrefix = %q, want TestParseGo", got)
	}
}

func TestAdapterFor(t *testing.T) {
	tests := []struct {
		lang    string
		wantErr bool
	}{
		{"go", false},
		{"java", false},
		{"python", false},
		{"typescript", false},
		{"", true},
	}
	for _, tt := range tests {
		_, err := AdapterFor(tt.lang)
		if (err != nil) != tt.wantErr {
			t.Errorf("AdapterFor(%q) err = %v, wantErr %v", tt.lang, err, tt.wantErr)
		}
	}
}
