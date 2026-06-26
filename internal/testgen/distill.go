package testgen

import "strings"

// failureMarkers identify error lines in Go, Maven, and Gradle output.
var failureMarkers = []string{
	"--- FAIL", "FAIL\t", "panic:", ".go:", // go test + go compile errors
	"[ERROR]", "FAILED", "error:", "Caused by:", // Maven / Gradle / javac
}

const fallbackTailLines = 60

// DistillFailure reduces raw runner output to the lines a model needs to
// repair the test, capped at maxBytes.
func DistillFailure(output string, maxBytes int) string {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		for _, m := range failureMarkers {
			if strings.Contains(ln, m) {
				kept = append(kept, ln)
				break
			}
		}
	}
	if len(kept) == 0 {
		start := len(lines) - fallbackTailLines
		if start < 0 {
			start = 0
		}
		kept = lines[start:]
	}
	out := strings.Join(kept, "\n")
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return strings.TrimSpace(out)
}
