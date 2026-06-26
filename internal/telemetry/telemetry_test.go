package telemetry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/telemetry"
)

func TestLogger_Log_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	logger, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	entry := telemetry.Entry{
		Timestamp:      time.Now(),
		Provider:       "claude",
		Model:          "claude-sonnet-4-6",
		InputTokens:    100,
		OutputTokens:   50,
		LatencyMS:      1234,
		CostUSD:        0.001050,
		ToolCallsCount: 1,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file is empty after Log()")
	}

	var got telemetry.Entry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", got.Provider, "claude")
	}
	if got.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", got.InputTokens)
	}
}

func TestLogger_Log_AppendsMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	logger, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := logger.Log(telemetry.Entry{Provider: "claude", InputTokens: i}); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 newlines (3 JSONL entries), got %d", lines)
	}
}

func TestLogger_Log_CreatesDirectoryIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "telemetry.jsonl")

	logger, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if err := logger.Log(telemetry.Entry{Provider: "test"}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should exist after Log()")
	}
}

func TestLogger_ConcurrentLogsProduceValidLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	logger, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := logger.Log(telemetry.Entry{Provider: "mock", Model: "m", InputTokens: i}); err != nil {
				t.Errorf("Log: %v", err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading telemetry file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}
	for i, line := range lines {
		var e telemetry.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("line %d is not valid JSON: %v\n%s", i, err, line)
		}
	}
}
