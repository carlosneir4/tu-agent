---
name: go-conventions
description: Go coding standards for this project. Use when writing, reviewing, or
  refactoring any .go file. Covers error handling, interfaces, context propagation,
  and package structure conventions.
paths:
  - "**/*.go"
---

# Go Conventions

## Error Handling
- Always wrap errors with context: `fmt.Errorf("userService.Create: %w", err)`
- Use `errors.Is()` / `errors.As()` to inspect errors — never compare strings
- Define sentinel errors at package level: `var ErrNotFound = errors.New("not found")`
- Never ignore errors silently with `_`

## Interfaces
- Declare interfaces in the **consumer** package, not the implementor
- Keep interfaces small: 1–3 methods (interface segregation)
- Accept interfaces, return concrete types

## Context
- `ctx context.Context` is always the **first parameter**
- Never store context in a struct field
- Always propagate context through call chains
- Use `context.WithTimeout` or `context.WithDeadline` at service boundaries

## Concurrency
- Prefer channels over shared memory for communication
- Use `sync.WaitGroup` for fan-out / fan-in patterns
- Use `sync.Mutex` only for simple shared state
- Always document goroutine ownership and lifetime

## Package Structure
- One responsibility per package
- Avoid circular imports — use interfaces to break cycles
- Unexported types stay unexported unless explicitly needed externally
- Group imports: stdlib / external / internal (with blank line between groups)

## Naming
- Exported names should be self-documenting without the package prefix
- Acronyms in names stay uppercase: `UserID`, `HTTPClient`, `JSONResponse`
- Avoid `util`, `common`, `helpers` package names — be specific
