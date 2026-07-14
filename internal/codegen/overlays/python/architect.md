## Python-specific design rules

- Use `dataclass` or `Pydantic` for structured data — not plain `dict` at API boundaries.
- Dependency injection: pass dependencies as constructor parameters. Avoid module-level mutable singletons.
- Async: choose sync or async consistently per module. Do not mix `asyncio` with blocking I/O.
- Packaging: `pyproject.toml` with `[build-system]` is the standard. `setup.py` is legacy.
- Type hints: all public functions and class methods must have full type annotations.
- Error handling: use specific exception classes. `except Exception: pass` is always wrong.
