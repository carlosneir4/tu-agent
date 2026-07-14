## Go-specific rules

- Wrap errors with context: `fmt.Errorf("pkg.Func: %w", err)`. Never swallow errors.
- No `panic()` in library code; only `main` may panic on unrecoverable startup errors.
- Define interfaces in the consumer package, not the producer.
- Context is the first parameter: `ctx context.Context`.
- Pre-allocate slices when the size is known: `make([]T, 0, n)`.
- Run `gofmt -w` and `go vet ./...` before finishing.
- Tests: table-driven, `*_test.go` co-located, mock external deps (no live APIs).
