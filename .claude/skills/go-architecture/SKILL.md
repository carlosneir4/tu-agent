---
name: go-architecture
description: Architecture decisions for new packages, services, or modules in this
  Go project. Use when scaffolding a new feature, service, internal package, or
  deciding how to structure a new domain area.
---

# Go Architecture

## Internal Feature Structure
Every new domain feature lives under `internal/<featurename>/`:

```
internal/
  user/
    handler.go        ← HTTP / gRPC handlers (I/O only, no business logic)
    handler_test.go
    service.go        ← business logic
    service_test.go
    repository.go     ← data access interface + implementation
    repository_test.go
    model.go          ← domain structs (no DB tags here)
```

## Dependency Rules
- `handler` depends on `service` **via interface**
- `service` depends on `repository` **via interface**
- Never import `internal/X` from `internal/Y` without an interface boundary
- `pkg/` packages must have zero imports from `internal/`

## Constructor Pattern
Always use constructor functions with dependency injection:

```go
// Define the interface the handler needs
type UserService interface {
    Create(ctx context.Context, email string) (*User, error)
    GetByID(ctx context.Context, id string) (*User, error)
}

// Constructor accepts the interface, returns concrete type
func NewUserHandler(svc UserService) *UserHandler {
    return &UserHandler{svc: svc}
}
```

## Configuration
- One `Config` struct per service, loaded at startup via `envconfig`
- Pass config by value (not pointer) into constructors
- Never read `os.Getenv` inside business logic — only in `main.go` / `cmd/`

## Database Access
- Repository interface defined in the domain package
- SQL implementation in `internal/<feature>/postgres.go`
- Use `pgx/v5` connection pool — never open a new connection per request
- Wrap DB errors into domain errors at the repository boundary

## HTTP Layer (Gin)
- Handlers only: bind request → call service → write response
- Validation at handler level using `binding` tags or explicit checks
- Return consistent error envelopes:

```go
type ErrorResponse struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

## Adding a New Service
1. Create `internal/<name>/` directory with the files above
2. Define interfaces before implementing
3. Write tests before or alongside implementation
4. Wire up in `cmd/<service>/main.go` via constructor injection
5. Add route registration in the router setup file
