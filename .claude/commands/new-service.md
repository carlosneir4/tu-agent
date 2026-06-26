---
description: Scaffold a new internal service following the project's architecture.
  Use when adding a new domain feature or microservice endpoint from scratch.
---

The user wants to create a new service. Ask for the service name if not provided,
then scaffold the following files following the go-architecture skill conventions:

## Files to create
Replace `<name>` with the lowercase service name (e.g. `payment`, `notification`):

1. `internal/<name>/model.go` — domain structs, no DB tags
2. `internal/<name>/repository.go` — Repository interface + pgx implementation stub
3. `internal/<name>/service.go` — Service interface + implementation with constructor
4. `internal/<name>/handler.go` — Gin handler with constructor, binds routes
5. `internal/<name>/service_test.go` — table-driven unit tests with gomock
6. `internal/<name>/handler_test.go` — handler tests with mock service

## Rules
- All files must compile with `go build ./...`
- Interfaces defined in the same package (consumer pattern)
- Constructor functions for every struct that has dependencies
- Every exported function has a godoc comment
- Run `go vet ./...` after scaffolding and fix any issues
- Remind the user to wire up the handler in `cmd/<service>/main.go`
