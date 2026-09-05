#!/usr/bin/env python3
"""Fail when the bug log contains duplicate record identifiers."""

from __future__ import annotations

import re
import sys
from collections import defaultdict
from pathlib import Path


RECORD_HEADING = re.compile(
    r"^## (?P<identifier>BUG-[A-Z0-9-]+|Bug #[A-Z0-9]+|AUDIT-[A-Z0-9-]+)\b"
)


def main() -> int:
    log_path = Path(__file__).resolve().parents[1] / "docs" / "bug-log.md"
    records: dict[str, list[int]] = defaultdict(list)

    for line_number, line in enumerate(log_path.read_text(encoding="utf-8").splitlines(), 1):
        match = RECORD_HEADING.match(line)
        if match:
            records[match.group("identifier").upper()].append(line_number)

    duplicates = {
        identifier: lines
        for identifier, lines in records.items()
        if len(lines) > 1
    }
    if not duplicates:
        print(f"bug log identifiers are unique ({len(records)} records)")
        return 0

    for identifier, lines in sorted(duplicates.items()):
        print(
            f"duplicate bug identifier {identifier}: lines "
            + ", ".join(str(line) for line in lines),
            file=sys.stderr,
        )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
