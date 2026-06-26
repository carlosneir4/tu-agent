"""Small text helpers — fixture for the testgen criterion run."""


def slugify(text: str) -> str:
    """Lowercase, trim, and join words with hyphens."""
    words = text.strip().lower().split()
    return "-".join(words)


def truncate(text: str, limit: int) -> str:
    """Cut text to limit characters, appending an ellipsis when cut."""
    if limit <= 0:
        return ""
    if len(text) <= limit:
        return text
    return text[: limit - 1] + "…"


def word_count(text: str) -> int:
    """Number of whitespace-separated words."""
    return len(text.split())
