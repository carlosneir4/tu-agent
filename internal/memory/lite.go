package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// liteObservation mirrors one entry of the legacy memory.json format
// (Memory Lite, Phase 0). Kept only as the migration source.
type liteObservation struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Topic     string    `json:"topic"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
}

// loadLiteObservations reads a legacy JSON memory file. A missing file
// returns (nil, nil); malformed JSON is an error.
func loadLiteObservations(path string) ([]liteObservation, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory.loadLiteObservations: read %s: %w", path, err)
	}
	var obs []liteObservation
	if err := json.Unmarshal(data, &obs); err != nil {
		return nil, fmt.Errorf("memory.loadLiteObservations: parse %s: %w", path, err)
	}
	return obs, nil
}
