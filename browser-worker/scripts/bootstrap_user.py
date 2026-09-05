"""Create or reset a local administrator account."""

from __future__ import annotations

import argparse
import getpass
import os

from sqlalchemy import select

from app.core.auth import hash_password
from app.db import get_session_factory
from app.models import User


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--email", required=True)
    parser.add_argument("--display-name", default="Administrator")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    email = args.email.strip().lower()
    password = os.getenv("AUTH_BOOTSTRAP_PASSWORD") or getpass.getpass("Password: ")
    if "@" not in email:
        raise SystemExit("email must be valid")
    if len(password) < 12:
        raise SystemExit("password must contain at least 12 characters")

    with get_session_factory()() as session:
        user = session.scalar(select(User).where(User.email == email))
        if user is None:
            user = User(
                email=email,
                display_name=args.display_name.strip() or "Administrator",
                password_hash=hash_password(password),
                is_active=True,
            )
            session.add(user)
            action = "created"
        else:
            user.display_name = args.display_name.strip() or user.display_name
            user.password_hash = hash_password(password)
            user.is_active = True
            action = "updated"
        session.commit()
        print(f"{action} user {email}")


if __name__ == "__main__":
    main()
