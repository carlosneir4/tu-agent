## Go review checklist

- Errors wrapped with `%w` and context; no silently swallowed errors.
- No `panic()` in library code.
- Context passed as first param; no `context.Background()` deep in call stacks.
- Interfaces defined in the consumer, kept small.
- Slices pre-allocated when size is known.
- `gofmt`/`go vet` clean; no obvious data races.
- Tests present for new handlers/services; table-driven where appropriate.
