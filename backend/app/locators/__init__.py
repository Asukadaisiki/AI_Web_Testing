"""Locators package."""

from app.locators.accessibility import (
    try_accessibility_locate,
    snapshot_accessibility_tree,
    flatten_interactive_nodes,
)
from app.locators.fallback import InterventionNeededError, resolve_with_fallback
from app.locators.semantic import LocatorResolutionError, ResolvedLocator, resolve_semantic_locator

__all__ = [
    "InterventionNeededError",
    "LocatorResolutionError",
    "ResolvedLocator",
    "resolve_semantic_locator",
    "resolve_with_fallback",
    "try_accessibility_locate",
    "snapshot_accessibility_tree",
    "flatten_interactive_nodes",
]
