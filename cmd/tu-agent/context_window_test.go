package main

import (
	"context"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/provider"
)

type winStub struct{ win int }

func (winStub) Name() string  { return "stub" }
func (winStub) Model() string { return "stub" }
func (winStub) Send(context.Context, string, []provider.Message, []provider.ToolDef) (provider.Response, error) {
	return provider.Response{}, nil
}
func (s winStub) NativeContextWindow() int { return s.win }

func TestEffectiveContextSize(t *testing.T) {
	p := winStub{win: 200000}
	if got := effectiveContextSize(16384, p); got != 16384 {
		t.Errorf("configured value should win: got %d, want 16384", got)
	}
	if got := effectiveContextSize(0, p); got != 200000 {
		t.Errorf("0 should fall back to native: got %d, want 200000", got)
	}
	if got := effectiveContextSize(-1, p); got != 200000 {
		t.Errorf("negative should fall back to native: got %d, want 200000", got)
	}
}
