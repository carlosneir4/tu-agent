## TypeScript-specific rules

- `tsconfig` must have `strict: true`. Never add `@ts-ignore` without a comment explaining why.
- Use `const` by default; `let` only when reassignment is needed.
- Avoid `any` — use `unknown` + type guard, or define a proper type.
- Import paths: use configured path aliases (`@/`) not deep relative paths (`../../..`).
- Tests: follow existing `*.test.ts` or `*.spec.ts` naming convention.
- Do not add new dependencies without checking bundle impact via the project's build command.
