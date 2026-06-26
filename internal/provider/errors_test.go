package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsPromptTooLargeMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Trying to keep the first 40000 tokens... exceeds the context length", true},
		{"This model's maximum context length is 32768 tokens", true},
		{"prompt is too long: 210000 tokens > 200000 maximum", true},
		{"the request exceeds the context window", true},
		// LM Studio flat-JSON format: the raw body contains the message
		{`{"error":"Context size has been exceeded."}`, true},
		{"Context size has been exceeded.", true},
		{"invalid api key", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isPromptTooLargeMessage(c.msg); got != c.want {
			t.Errorf("isPromptTooLargeMessage(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestErrPromptTooLargeIsMatchable(t *testing.T) {
	err := fmt.Errorf("provider.LocalAdapter.Send: http 400: ctx overflow: %w", ErrPromptTooLarge)
	if !errors.Is(err, ErrPromptTooLarge) {
		t.Errorf("wrapped sentinel not matchable with errors.Is")
	}
}
