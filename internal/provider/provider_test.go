package provider_test

import (
	"context"
	"testing"

	"github.com/tu/tu-agent/internal/provider"
)

// compile-time check: mockProvider must satisfy the Provider interface
var _ provider.Provider = (*mockProvider)(nil)

type mockProvider struct{}

func (m *mockProvider) Send(
	_ context.Context,
	_ string,
	_ []provider.Message,
	_ []provider.ToolDef,
) (provider.Response, error) {
	return provider.Response{}, nil
}

func (m *mockProvider) Name() string             { return "mock" }
func (m *mockProvider) Model() string            { return "mock-model" }
func (m *mockProvider) NativeContextWindow() int { return 200000 }

func TestNativeContextWindow(t *testing.T) {
	c := provider.NewClaudeAdapter(provider.ClaudeConfig{APIKey: "x", Model: "claude-sonnet-4-6"})
	if got := c.NativeContextWindow(); got != 200000 {
		t.Errorf("claude NativeContextWindow() = %d, want 200000", got)
	}
	q := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: "http://x", Model: "m"})
	if got := q.NativeContextWindow(); got != 8192 {
		t.Errorf("local NativeContextWindow() = %d, want 8192", got)
	}
}
