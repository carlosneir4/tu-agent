package telemetry_test

import (
	"testing"

	"github.com/tu/tu-agent/internal/telemetry"
)

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		model        string
		inputTokens  int
		outputTokens int
		wantMin      float64
		wantMax      float64
	}{
		{
			name:         "haiku known pricing",
			provider:     "claude",
			model:        "claude-haiku-4-5-20251001",
			inputTokens:  1000,
			outputTokens: 500,
			// 1000 * 0.80/1e6 + 500 * 4.00/1e6 = 0.0008 + 0.002 = 0.0028
			wantMin: 0.00279,
			wantMax: 0.00281,
		},
		{
			name:         "local is free",
			provider:     "local",
			model:        "qwen/qwen3-coder-30b",
			inputTokens:  10000,
			outputTokens: 5000,
			wantMin:      0.0,
			wantMax:      0.0,
		},
		{
			name:         "unknown model is free",
			provider:     "claude",
			model:        "claude-unknown-9",
			inputTokens:  1000,
			outputTokens: 500,
			wantMin:      0.0,
			wantMax:      0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := telemetry.EstimateCost(tt.provider, tt.model, tt.inputTokens, tt.outputTokens)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateCost(%q, %q, %d, %d) = %f, want [%f, %f]",
					tt.provider, tt.model, tt.inputTokens, tt.outputTokens, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}
