package provider

import (
	"errors"
	"strings"
)

// ErrPromptTooLarge marks a request rejected because the prompt exceeded the
// model's context window. Adapters wrap their provider-specific variant of
// this condition with the sentinel so callers can degrade (e.g. fall back to
// map-reduce) via errors.Is.
var ErrPromptTooLarge = errors.New("prompt exceeds model context window")

// promptTooLargeMarkers are lowercase substrings that OpenAI-compatible
// servers (LM Studio, vLLM) and the Anthropic API use when rejecting an
// oversized prompt.
var promptTooLargeMarkers = []string{
	"context length",
	"context window",
	"context size", // LM Studio: "Context size has been exceeded."
	"maximum context",
	"prompt is too long",
	"too many tokens",
}

func isPromptTooLargeMessage(msg string) bool {
	m := strings.ToLower(msg)
	for _, marker := range promptTooLargeMarkers {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}
