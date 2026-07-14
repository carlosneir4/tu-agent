package main

import (
	"log/slog"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// withMemStore opens the project memory store at root, runs fn against it, and
// closes it afterwards — centralizing the open/defer-Close/scoped-body dance
// that the memory CLI commands and MCP handlers repeat. It returns fn's error
// unchanged (or the open error if the store could not be opened). A Close
// failure is logged via slog.Warn but never masks fn's error, matching the
// existing call sites' defer-Close behavior while making it consistent.
func withMemStore(root string, fn func(*memory.Store) error) error {
	st, err := memory.Open(memoryDBPath(root))
	if err != nil {
		return err
	}
	return runWithStoreClose(st, func() error { return fn(st) })
}

// runWithStoreClose runs fn, then closes st, logging a slog.Warn if Close fails
// WITHOUT masking fn's error. Split out from withMemStore so the Close-failure
// path is reachable in a test with a fake store (withMemStore itself needs a
// real *memory.Store, whose Close does not fail on demand). The deferred Close
// runs after fn's return value is fixed, so the returned error is always fn's.
func runWithStoreClose(st interface{ Close() error }, fn func() error) error {
	defer func() {
		if cerr := st.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	return fn()
}
