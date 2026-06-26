package main

import "github.com/tu/tu-agent/internal/provider"

// effectiveContextSize returns the token window to budget against: the
// configured context_size when set (> 0), otherwise the provider's native
// window. Local providers configure context_size to match their server; remote
// providers (Claude, future Gemini) fall back to their declared native window.
func effectiveContextSize(contextSize int, prov provider.Provider) int {
	if contextSize > 0 {
		return contextSize
	}
	return prov.NativeContextWindow()
}
