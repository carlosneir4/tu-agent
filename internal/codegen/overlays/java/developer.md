## Java-specific rules

- Wrap errors: `throw new RuntimeException("ServiceName.methodName: " + e.getMessage(), e)`.
- Use `Optional<T>` for return values that may be absent. Never return `null` from a public method.
- Module-scoped build: `mvn -pl <module> -am test` to avoid full-monolith compile.
- Tests: `src/test/java`, mirror the main package structure. Use JUnit 5 + Mockito.
- Use `var` for local variables when the type is obvious from the right-hand side.
- Prefer `stream().filter().map().collect()` over imperative loops for transformations.
- Do not upgrade dependency versions unless explicitly asked.
