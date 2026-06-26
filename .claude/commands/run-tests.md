---
description: Run tests for the project or a specific package. Supports unit tests,
  race detection, coverage reports, and integration tests.
---

Determine the scope from the user's request:

- **All tests**: `go test ./... -race -count=1 -timeout 120s`
- **Specific package**: `go test ./<package>/... -race -v -count=1`
- **With coverage**: add `-coverprofile=coverage.out` then `go tool cover -html=coverage.out -o coverage.html`
- **Integration tests**: `go test -tags=integration ./... -timeout 300s`
- **Single test by name**: `go test ./... -run TestName -v`

After running:
1. Report total pass/fail count
2. List any failing tests with their error output
3. If coverage was requested, report total coverage percentage
4. Suggest which failing tests to investigate first based on the error messages
