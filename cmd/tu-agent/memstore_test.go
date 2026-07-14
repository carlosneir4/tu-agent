package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestWithMemStore_SuccessRunsBodyAndCloses verifies the helper opens a live
// store, hands it to the body, returns nil on success, and closes the store
// afterwards (a second open of the same path must succeed).
func TestWithMemStore_Success(t *testing.T) {
	t.Chdir(t.TempDir())

	var got *memory.Store
	err := withMemStore(repoRoot(), func(s *memory.Store) error {
		if s == nil {
			t.Fatal("body received a nil *memory.Store")
		}
		got = s
		// Exercise the store to prove it is live.
		if _, uerr := s.Upsert("gotcha/x", "content", memory.UpsertOpts{}); uerr != nil {
			t.Fatalf("upsert against live store: %v", uerr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withMemStore returned %v, want nil", err)
	}
	if got == nil {
		t.Fatal("body was not run")
	}
	// Store must be closed now: reopening the same path must succeed.
	s2, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatalf("reopen after withMemStore: %v", err)
	}
	_ = s2.Close()
}

// TestWithMemStore_PropagatesBodyError verifies the helper returns the body's
// error unchanged.
func TestWithMemStore_PropagatesBodyError(t *testing.T) {
	t.Chdir(t.TempDir())

	sentinel := errors.New("body boom")
	err := withMemStore(repoRoot(), func(*memory.Store) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("withMemStore returned %v, want the body sentinel", err)
	}
}

// closeErrStore is a fake whose Close always fails, driving the Close-failure
// path of runWithStoreClose without needing a real store that fails on Close.
type closeErrStore struct{ err error }

func (c closeErrStore) Close() error { return c.err }

// TestRunWithStoreClose_CloseFailureDoesNotMaskBodyError verifies that when the
// body returns an error AND Close fails, the returned error is the body's
// error (not the Close error) and the Close failure is surfaced via slog.Warn.
func TestRunWithStoreClose_CloseFailureDoesNotMaskBodyError(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	closeErr := errors.New("close boom")
	bodyErr := errors.New("body boom")

	got := runWithStoreClose(closeErrStore{err: closeErr}, func() error {
		return bodyErr
	})

	if !errors.Is(got, bodyErr) {
		t.Fatalf("returned %v, want the body error", got)
	}
	if errors.Is(got, closeErr) {
		t.Fatal("returned error masked the body error with the Close error")
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "memory store close failed") {
		t.Errorf("expected a slog.Warn about the Close failure, got: %q", logged)
	}
	if !strings.Contains(logged, "close boom") {
		t.Errorf("expected the Close error in the warning, got: %q", logged)
	}
}

// TestRunWithStoreClose_CloseFailureWithNilBodyError verifies that when the body
// succeeds but Close fails, the Close error is still not propagated (only warned)
// — closing is best-effort observability, never a returned failure.
func TestRunWithStoreClose_CloseFailureWithNilBodyError(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got := runWithStoreClose(closeErrStore{err: errors.New("close boom")}, func() error {
		return nil
	})
	if got != nil {
		t.Fatalf("returned %v, want nil (Close error must not surface)", got)
	}
	if !strings.Contains(logBuf.String(), "memory store close failed") {
		t.Errorf("expected a slog.Warn about the Close failure, got: %q", logBuf.String())
	}
}
