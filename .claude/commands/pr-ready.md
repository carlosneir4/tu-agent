---
description: Prepare the current branch for a pull request. Runs linting, tests,
  coverage, and produces a summary of changes ready to paste into the PR description.
---

Run the following steps in order and report the result of each:

1. `go vet ./...`
2. `golangci-lint run ./...`
3. `go test ./... -race -coverprofile=coverage.out -timeout 120s`
4. `go tool cover -func=coverage.out | tail -1`
5. `git log main..HEAD --oneline`
6. `git diff main --stat`

Then produce a PR description with:
- **Summary**: what this branch does in 2–3 sentences
- **Changes**: list of files changed and why
- **Coverage**: total coverage percentage from step 4
- **Lint**: PASS or list of warnings to address before merging
- **How to test**: manual steps a reviewer should follow

If any step fails, stop and fix it before continuing.
