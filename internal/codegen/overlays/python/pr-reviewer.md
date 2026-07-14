## Python-specific review checks

- **Type annotations**: are new functions and methods fully typed? Are `Any` types documented?
- **Exception handling**: are exceptions specific? Are `except` clauses too broad (`except Exception:`)?
- **Resource management**: are file/network/DB operations using context managers (`with`)?
- **Mutable defaults**: any `def f(x=[])` or `def f(x={})` — these are bugs, flag as CRITICAL.
- **Import organization**: stdlib → third-party → local, separated by blank lines (PEP8/isort).
- **Type guards**: are `Optional` and `Union` types properly narrowed before use?
