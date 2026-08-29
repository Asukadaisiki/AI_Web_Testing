"""Planning execution input resolution."""

from __future__ import annotations

import logging
from typing import Any


from app.core.structured_logging import get_structured_logger



logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


def build_input_values_from_session(
    requirements_json: dict[str, Any],
    dsl_case_jsons: list[dict[str, Any] | None],
) -> dict[str, str]:
    """Read input_values from contract defaults, with heuristic fallback for legacy cases.

    New DSL generator populates input_contract[].value directly from the user's
    test data, so this function primarily just reads those values.  The heuristic
    parsing of test_data_or_account text is kept as a fallback for old drafts.
    """
    result: dict[str, str] = {}

    # Primary: read values directly from contract defaults (new generator path)
    for case_json in dsl_case_jsons:
        if not case_json:
            continue
        for ic in case_json.get("input_contract", []) or []:
            key = (ic.get("context_key") or "").strip()
            val = ic.get("value")
            if key and val is not None and str(val).strip():
                result[key] = str(val).strip()

    if result:
        logger.info("[_build_input_values] Read %d values from contract defaults: %s",
                     len(result), {k: v[:10] for k, v in result.items()})
        return result

    # Legacy fallback: parse test_data_or_account text for old drafts
    import re
    raw = (requirements_json.get("test_data_or_account") or "").strip()
    if not raw:
        return result

    # Collect context_keys from contracts (for matching)
    context_keys: set[str] = set()
    for case_json in dsl_case_jsons:
        if not case_json:
            continue
        for ic in case_json.get("input_contract", []) or []:
            key = (ic.get("context_key") or "").strip()
            if key:
                context_keys.add(key)

    # Simple key:value pair extraction
    _CN_KEY_MAP: dict[str, list[str]] = {
        "账号": ["email", "username", "login"], "邮箱": ["email", "mail"],
        "用户名": ["username", "user", "login"], "密码": ["password", "pass", "pwd"],
        "口令": ["password", "pass", "pwd"],
    }
    pairs: dict[str, str] = {}
    for entry in re.split(r"[\n,，;；]+", raw):
        entry = re.sub(r'^\d+\.\s*', '', entry.strip())
        m = re.match(r"(.+?)[：:=]\s*(.+)", entry) if entry else None
        if m:
            pairs[m.group(1).strip()] = m.group(2).strip()
        elif entry and "@" in entry:
            pairs.setdefault("email", entry)

    for ck in context_keys:
        ck_lower = ck.lower()
        for label, value in pairs.items():
            label_lower = label.lower()
            if ck_lower in label_lower or label_lower in ck_lower:
                result[ck] = value
                break
            for cn_key, en_keys in _CN_KEY_MAP.items():
                if cn_key in label_lower and ck_lower in en_keys:
                    result[ck] = value
                    break
            else:
                continue
            break
        else:
            if "email" in ck_lower or "mail" in ck_lower:
                for v in pairs.values():
                    if "@" in v:
                        result[ck] = v
                        break
            elif "password" in ck_lower or "pass" in ck_lower or "pwd" in ck_lower:
                for v in pairs.values():
                    if "@" not in v and len(v) >= 4:
                        result[ck] = v
                        break

    if result:
        logger.info("[_build_input_values] Legacy fallback resolved: %s",
                     {k: v[:10] for k, v in result.items()})
    return result


