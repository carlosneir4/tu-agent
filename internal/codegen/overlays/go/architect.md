## Go architecture rules

- Public packages are small and well-named. Avoid `internal/util` or `internal/common` dumping grounds.
- `cmd/` holds entrypoints (one `main.go` per binary); `internal/` holds private logic; `pkg/` holds externally-safe reusable code.
- Define interfaces in the consumer package. Keep them minimal.
- Prefer the standard library; justify each third-party dependency.
- Files that change together live together; split by responsibility, not technical layer.
