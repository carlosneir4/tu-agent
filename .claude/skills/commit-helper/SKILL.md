---
name: commit-helper
description: Generate Conventional Commits messages for this project. Use when the
  user asks to commit changes, write a commit message, or prepare staged changes for git.
---

# Commit Helper

## Format
```
<type>(<scope>): <short description>

[optional body — what changed and why, not how]
[optional footer — breaking changes, issue refs]
```

## Allowed Types
| Type       | When to use                                      |
|------------|--------------------------------------------------|
| `feat`     | New feature or behavior                          |
| `fix`      | Bug fix                                          |
| `refactor` | Code restructure with no functional change       |
| `test`     | Adding or updating tests                         |
| `chore`    | Dependency updates, CI config, tooling           |
| `docs`     | Documentation only                               |
| `perf`     | Performance improvement                          |
| `build`    | Build system changes (Makefile, Dockerfile, etc) |

## Scope Examples (match package or layer)
`auth`, `user`, `handler`, `repository`, `middleware`, `config`, `ci`

## Rules
- Description in English, imperative mood, ≤72 characters
- No period at the end of the subject line
- Body explains *why*, not *what* (the diff shows what)
- Breaking changes go in footer: `BREAKING CHANGE: <description>`

## Process
1. Run `git diff --staged` to review staged changes
2. Identify the type and scope from the changes
3. Write the subject line — imperative, concise
4. Add body only if the *why* is not obvious
5. Reference tickets in footer: `Closes #123`

## Good Examples
```
feat(auth): add JWT refresh token rotation
fix(user): return 404 instead of 500 when user not found
refactor(repository): extract query builder into separate method
test(handler): add table-driven tests for CreateUser endpoint
chore(deps): upgrade pgx to v5.6.0
```
