package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Logger appends telemetry entries to a JSONL file.
// Safe for concurrent use: Log serializes writes with an internal mutex.
type Logger struct {
	mu   sync.Mutex
	path string
}

// NewLogger creates a Logger that writes to the given path.
// The file and its parent directories are created on the first Log call.
func NewLogger(path string) (*Logger, error) {
	return &Logger{path: path}, nil
}

// Log appends one entry as a JSON line to the telemetry file.
func (l *Logger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("telemetry.Log: mkdir: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry.Log: open file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(entry); err != nil {
		return fmt.Errorf("telemetry.Log: encode: %w", err)
	}
	return nil
}
