---
name: go-testing
description: Testing patterns for this Go project. Use when writing unit tests,
  integration tests, table-driven tests, or generating mocks with gomock and testify.
---

# Go Testing

## Test Structure
Always use the Arrange / Act / Assert pattern:

```go
func TestServiceName_MethodName(t *testing.T) {
    t.Run("should return error when input is invalid", func(t *testing.T) {
        // Arrange
        svc := NewUserService(mockRepo)

        // Act
        result, err := svc.Create(ctx, "")

        // Assert
        require.Error(t, err)
        assert.ErrorIs(t, err, ErrInvalidInput)
        assert.Nil(t, result)
    })
}
```

## Table-Driven Tests
Required whenever there are 2 or more cases:

```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {name: "valid input",  input: "foo", want: "bar", wantErr: false},
    {name: "empty input",  input: "",    want: "",    wantErr: true},
    {name: "special chars",input: "@#!",  want: "",    wantErr: true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := fn(tt.input)
        if tt.wantErr {
            require.Error(t, err)
            return
        }
        require.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

## Mocks with gomock
- Generate mocks: add `//go:generate mockgen -source=service.go -destination=../mocks/mock_service.go`
- Store all mocks under `internal/mocks/`
- Never commit generated mocks if CI regenerates them

```go
func TestHandler_CreateUser(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockSvc := mocks.NewMockUserService(ctrl)
    mockSvc.EXPECT().
        Create(gomock.Any(), "alice@example.com").
        Return(&User{ID: "1"}, nil).
        Times(1)

    h := NewHandler(mockSvc)
    // ... test the handler
}
```

## Test Helpers
- Use `t.Helper()` in helper functions so failures point to the call site
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal
- Prefer `t.Cleanup()` over `defer` for resource teardown

## Integration Tests
- Tag with `//go:build integration`
- Run with: `go test -tags=integration ./...`
- Use a real DB via Docker in CI — no sqlite shortcuts
