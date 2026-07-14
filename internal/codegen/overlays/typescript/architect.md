## TypeScript-specific design rules

- Define types at the boundary: API responses, event payloads, config shapes — not inside functions.
- Prefer `interface` over `type` for object shapes that may be extended.
- Use `unknown` instead of `any`. If `any` is truly needed, add a comment explaining why.
- Module boundaries: barrel files (`index.ts`) define the public API; internal modules are unexported.
- Async: `async/await` over `.then()` chains. Handle rejections with `try/catch` at the boundary.
- Shared state: prefer immutable data structures. Avoid module-level mutable singletons.
