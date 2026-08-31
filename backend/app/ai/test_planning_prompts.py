"""Compatibility helpers for planning prompts.

Prompt definitions live in ``app.ai.prompts``. Keep this module as the stable
import path for existing planning-agent code while prompt ownership is migrated.
"""

from __future__ import annotations

from app.ai.planning_tools import get_tool_descriptions_for_prompt
from app.ai.prompts import FORCE_GENERATE_HINT, FORCE_GENERATE_MARKER, PromptStage, render_prompt
from app.ai.prompts.registry import PLANNING_INIT_TEMPLATE


SYSTEM_PROMPT_TEMPLATE = PLANNING_INIT_TEMPLATE

__all__ = [
    "FORCE_GENERATE_HINT",
    "FORCE_GENERATE_MARKER",
    "SYSTEM_PROMPT_TEMPLATE",
    "build_system_prompt",
]


def build_system_prompt() -> str:
    """Build the full system prompt with current tool descriptions."""
    rendered = render_prompt(
        PromptStage.PLANNING_INIT,
        {"tool_descriptions": get_tool_descriptions_for_prompt()},
    )
    return rendered.content
