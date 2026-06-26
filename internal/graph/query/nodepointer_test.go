package query

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

func TestNodePointerIncludesSignature(t *testing.T) {
	tests := []struct {
		name string
		node graph.Node
		want string
	}{
		{
			name: "function with params and return",
			node: graph.Node{
				Kind: graph.KindFunction, Name: "Service.Process", Path: "billing/svc.go",
				Line: 5, EndLine: 9, Params: "(invoice Invoice)", ReturnType: "error",
			},
			want: "billing/svc.go:5-9 (function Service.Process(invoice Invoice) error)",
		},
		{
			name: "function without return type",
			node: graph.Node{
				Kind: graph.KindFunction, Name: "helper", Path: "billing/svc.py",
				Line: 6, EndLine: 7, Params: "(x)",
			},
			want: "billing/svc.py:6-7 (function helper(x))",
		},
		{
			name: "node without signature is unchanged",
			node: graph.Node{
				Kind: graph.KindClass, Name: "Service", Path: "billing/svc.go",
				Line: 3, EndLine: 3,
			},
			want: "billing/svc.go:3-3 (class Service)",
		},
		{
			name: "function with Line==0 uses path only",
			node: graph.Node{
				Kind: graph.KindFunction, Name: "Init", Path: "billing/svc.go",
				Params: "()", ReturnType: "error",
			},
			want: "billing/svc.go (function Init() error)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nodePointer(tc.node); got != tc.want {
				t.Errorf("nodePointer = %q, want %q", got, tc.want)
			}
		})
	}
}
