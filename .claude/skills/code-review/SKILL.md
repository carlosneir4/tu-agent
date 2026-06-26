---
name: code-review
description: Perform a thorough Go code review. Use when asked to review, audit,
  check quality, correctness, or security of Go files or a pull request diff.
---

# Go Code Review

## Correctness
- [ ] All errors are handled — no silent `_` on error returns
- [ ] Nil pointer dereferences are guarded before use
- [ ] Slice/map bounds are validated before access
- [ ] Goroutines have a clear owner and defined exit path
- [ ] No race conditions on shared state (run `go test -race -tags sqlite_fts5 ./...`)
- [ ] Deferred functions execute in the right order

## Error Handling Quality
- [ ] Errors wrapped with meaningful context (`fmt.Errorf("pkg.Func: %w", err)`)
- [ ] Sentinel errors used correctly with `errors.Is` / `errors.As`
- [ ] HTTP handlers return correct status codes for each error type
- [ ] DB / external service errors mapped to domain errors at the boundary

## Security
- [ ] No secrets or credentials hardcoded anywhere
- [ ] SQL queries use parameterized statements — no string concatenation
- [ ] HTTP inputs are validated and sanitized before processing
- [ ] File paths are not constructed from user input without validation
- [ ] Sensitive data (passwords, tokens) not logged

## Performance
- [ ] Slices pre-allocated with `make([]T, 0, n)` when size is known
- [ ] No unnecessary allocations in hot paths (check for `+` string concat in loops)
- [ ] DB queries use the connection pool — no new connections per request
- [ ] N+1 query patterns avoided — batch where possible
- [ ] Context timeouts set on external calls

## Go Idioms
- [ ] `defer` used for cleanup (file close, mutex unlock, DB tx rollback)
- [ ] Receiver consistency — all methods use pointer OR value receiver, not both
- [ ] Exported identifiers are properly documented with godoc comments
- [ ] Package-level vars are only used for errors and compile-time interface checks
- [ ] `init()` functions avoided unless absolutely necessary

## Architecture
- [ ] Interfaces defined in the consumer package
- [ ] Business logic lives in the service layer, not handlers or repositories
- [ ] No circular imports introduced
- [ ] New code follows the existing package structure

## Tests
- [ ] New behavior has test coverage
- [ ] Tests use table-driven format for multiple cases
- [ ] Test names are descriptive: `TestService_Method_WhenCondition`
- [ ] No test logic leaks into production code
