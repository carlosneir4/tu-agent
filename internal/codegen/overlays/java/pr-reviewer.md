## Java-specific review checks

- **Null safety**: are public methods returning `Optional` or documented `@NonNull`?
- **Exception handling**: are checked exceptions wrapped with context? Are exceptions swallowed silently?
- **Resource management**: are `Closeable` resources in try-with-resources?
- **Thread safety**: is shared mutable state or static fields documented as thread-safe?
- **Serialization**: if a `Serializable` class changed, is `serialVersionUID` updated?
