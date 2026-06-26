package telemetry

var inputUSDPerToken = map[string]float64{
	"claude/claude-haiku-4-5-20251001": 0.80 / 1_000_000,
	"claude/claude-sonnet-4-6":         3.00 / 1_000_000,
	"claude/claude-opus-4-8":           15.00 / 1_000_000,
}

var outputUSDPerToken = map[string]float64{
	"claude/claude-haiku-4-5-20251001": 4.00 / 1_000_000,
	"claude/claude-sonnet-4-6":         15.00 / 1_000_000,
	"claude/claude-opus-4-8":           75.00 / 1_000_000,
}

// EstimateCost returns the estimated USD cost for a model call.
// Returns 0 for unknown or self-hosted providers (e.g., local).
func EstimateCost(providerName, model string, inputTokens, outputTokens int) float64 {
	key := providerName + "/" + model
	return inputUSDPerToken[key]*float64(inputTokens) +
		outputUSDPerToken[key]*float64(outputTokens)
}
