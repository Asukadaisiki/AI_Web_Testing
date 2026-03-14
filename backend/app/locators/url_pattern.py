"""Helpers for matching correction records against dynamic URLs."""

from __future__ import annotations

import re
from urllib.parse import urlparse


UUID_PATTERN = re.compile(
    r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
    re.IGNORECASE,
)


def generalize_url(url: str) -> str:
    parsed = urlparse(url)
    path_segments = parsed.path.split("/")
    generalized_segments = ["*" if _is_dynamic_segment(segment) else segment for segment in path_segments]
    generalized_path = "/".join(generalized_segments)
    return f"{parsed.scheme}://{parsed.netloc}{generalized_path}"


def _is_dynamic_segment(segment: str) -> bool:
    if not segment:
        return False
    if segment.isdigit():
        return True
    if UUID_PATTERN.match(segment):
        return True
    if len(segment) >= 16 and segment.isalnum():
        return True
    return False
