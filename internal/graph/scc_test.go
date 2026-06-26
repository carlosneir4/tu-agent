package graph

import (
	"reflect"
	"testing"
)

func TestStronglyConnectedComponents(t *testing.T) {
	tests := []struct {
		name string
		adj  map[string][]string
		want [][]string // sorted: size desc then first member; members sorted
	}{
		{
			name: "two-node cycle",
			adj:  map[string][]string{"a": {"b"}, "b": {"a"}},
			want: [][]string{{"a", "b"}},
		},
		{
			name: "DAG is all singletons",
			adj:  map[string][]string{"a": {"b"}, "b": {"c"}},
			want: [][]string{{"a"}, {"b"}, {"c"}},
		},
		{
			name: "three-node cycle plus a leaf",
			adj:  map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}, "a2": {"a"}},
			want: [][]string{{"a", "b", "c"}, {"a2"}},
		},
		{
			name: "two separate cycles",
			adj:  map[string][]string{"a": {"b"}, "b": {"a"}, "x": {"y"}, "y": {"x"}},
			want: [][]string{{"a", "b"}, {"x", "y"}},
		},
		{
			name: "target-only node included as singleton",
			adj:  map[string][]string{"a": {"z"}},
			want: [][]string{{"a"}, {"z"}},
		},
		{
			name: "self-loop is a singleton component",
			adj:  map[string][]string{"a": {"a"}},
			want: [][]string{{"a"}},
		},
		{
			name: "empty",
			adj:  map[string][]string{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StronglyConnectedComponents(tt.adj)
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStronglyConnectedComponents_Deterministic(t *testing.T) {
	adj := map[string][]string{"c": {"a"}, "a": {"b"}, "b": {"c"}, "z": {}, "m": {"z"}}
	first := StronglyConnectedComponents(adj)
	for i := 0; i < 5; i++ {
		if !reflect.DeepEqual(StronglyConnectedComponents(adj), first) {
			t.Fatalf("non-deterministic output")
		}
	}
}
