"""Shared Browser Worker service errors."""


class EntityNotFoundError(ValueError):
    """Raised when a required persisted entity does not exist."""
