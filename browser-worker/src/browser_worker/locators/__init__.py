"""Locators package."""

from browser_worker.locators.fallback import InterventionNeededError, resolve_with_fallback
from browser_worker.locators.semantic import LocatorResolutionError, ResolvedLocator, resolve_semantic_locator

__all__ = [
    "InterventionNeededError",
    "LocatorResolutionError",
    "ResolvedLocator",
    "resolve_semantic_locator",
    "resolve_with_fallback",
]
