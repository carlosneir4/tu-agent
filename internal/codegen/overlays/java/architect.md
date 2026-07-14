## Java-specific design rules

- Prefer constructor injection over field `@Autowired`. Document why if you deviate.
- Define interfaces for all service dependencies; implementations must not leak into consumers.
- New modules follow the established package structure: `<base-package>.<module>.<layer>`.
- Checked exceptions: if part of the API contract, declare them; if implementation detail, wrap in `RuntimeException` with context.
- Avoid static utility classes. If you find one, save it to memory as a gotcha.
- For new persistence: repository pattern behind an interface, not DAO scattered across services.
