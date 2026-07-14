## Python-specific rules

- Type hints required on all new functions and class methods.
- Use `pathlib.Path` not `os.path` for file operations.
- Exceptions: raise specific exceptions with context. Never `except Exception: pass`.
- Use context managers (`with`) for file I/O, database connections, and locks.
- f-strings for string formatting — not `%` or `.format()`.
- Activate virtual environment before running commands. Dependencies in `pyproject.toml`.
- Mutable default arguments are a bug: `def f(x=[])` → use `def f(x=None): x = x or []`.
