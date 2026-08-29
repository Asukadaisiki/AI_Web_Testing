"""Export the FastAPI OpenAPI schema for frontend type generation."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    args = parser.parse_args()

    os.environ.setdefault(
        "AUTH_SESSION_SECRET",
        "openapi-schema-generation-only",
    )

    from app.main import create_app

    args.output.write_text(
        json.dumps(create_app().openapi(), ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
