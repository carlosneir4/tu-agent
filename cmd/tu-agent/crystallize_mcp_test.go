package main

import (
	"context"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestHandleMemClusters_ReturnsClusters(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := ms.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	_, out, err := handleMemClusters(context.Background(), nil, memClustersMCPInput{Min: 3})
	if err != nil {
		t.Fatalf("handleMemClusters: %v", err)
	}
	if !strings.Contains(out.Result, "checkout") {
		t.Errorf("expected a checkout cluster, got:\n%s", out.Result)
	}
}

func TestMemClustersInMCPToolNames(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["mem_clusters"] {
		t.Error("newMCPServer does not serve mem_clusters")
	}
}
