"""Script to clean up orphaned data in the database.

Orphaned data includes:
- Projects not linked to any session
- Messages without a session
- Drafts without a session
- Event logs without a session

Usage:
    uv run python scripts/cleanup_orphan_data.py
    uv run python scripts/cleanup_orphan_data.py --execute --confirm DELETE_ORPHANED_DATA
"""

import sys
import argparse
from sqlalchemy import select

# Add the project root to the path
sys.path.insert(0, ".")

from app.db.session import get_session_factory
from app.models import (
    AIPlanningSession,
    AIPlanningMessage,
    AIPlanningDraft,
    AIPlanningEventLog,
    Project,
    ProjectMember,
    SessionProject,
    TestCase,
)


def find_orphaned_projects(session):
    """Find empty, non-default projects that are not linked to any session."""
    return session.scalars(
        select(Project).where(
            Project.is_default.is_(False),
            ~select(SessionProject.id).where(SessionProject.project_id == Project.id).exists(),
            ~select(ProjectMember.id).where(ProjectMember.project_id == Project.id).exists(),
            ~select(TestCase.id).where(TestCase.project_id == Project.id).exists(),
        )
    ).all()


def find_orphaned_messages(session):
    """Find messages without a session."""
    all_messages = session.query(AIPlanningMessage).all()
    all_session_ids = set(session.scalars(select(AIPlanningSession.id)).all())
    return [m for m in all_messages if m.session_id not in all_session_ids]


def find_orphaned_drafts(session):
    """Find drafts without a session."""
    all_drafts = session.query(AIPlanningDraft).all()
    all_session_ids = set(session.scalars(select(AIPlanningSession.id)).all())
    return [d for d in all_drafts if d.session_id not in all_session_ids]


def find_orphaned_event_logs(session):
    """Find event logs without a session."""
    try:
        all_logs = session.query(AIPlanningEventLog).all()
        all_session_ids = set(session.scalars(select(AIPlanningSession.id)).all())
        return [log for log in all_logs if log.session_id not in all_session_ids]
    except Exception:
        return []


def find_orphaned_session_project_links(session):
    """Find session-project links without a session or project."""
    all_links = session.query(SessionProject).all()
    all_session_ids = set(session.scalars(select(AIPlanningSession.id)).all())
    all_project_ids = set(session.scalars(select(Project.id)).all())
    return [
        link for link in all_links
        if link.session_id not in all_session_ids or link.project_id not in all_project_ids
    ]


def cleanup_orphaned_data(*, dry_run=True, confirmation=None):
    """Clean up all orphaned data."""
    if not dry_run and confirmation != "DELETE_ORPHANED_DATA":
        raise ValueError("Deletion requires confirmation='DELETE_ORPHANED_DATA'.")

    session_factory = get_session_factory()

    with session_factory() as session:
        # Find orphaned data
        orphaned_projects = find_orphaned_projects(session)
        orphaned_messages = find_orphaned_messages(session)
        orphaned_drafts = find_orphaned_drafts(session)
        orphaned_event_logs = find_orphaned_event_logs(session)
        orphaned_links = find_orphaned_session_project_links(session)

        # Print summary
        print("=== Orphaned Data Summary ===")
        print(f"Orphaned projects: {len(orphaned_projects)}")
        print(f"Orphaned messages: {len(orphaned_messages)}")
        print(f"Orphaned drafts: {len(orphaned_drafts)}")
        print(f"Orphaned event logs: {len(orphaned_event_logs)}")
        print(f"Orphaned session-project links: {len(orphaned_links)}")

        if dry_run:
            print("\n=== DRY RUN - No changes made ===")
            if orphaned_projects:
                print("\nOrphaned projects that would be deleted:")
                for p in orphaned_projects[:10]:
                    print(f"  - ID: {p.id}, Name: {p.name}")
                if len(orphaned_projects) > 10:
                    print(f"  ... and {len(orphaned_projects) - 10} more")
            return

        # Delete orphaned data
        print("\n=== Cleaning up orphaned data ===")

        # Delete orphaned session-project links
        for link in orphaned_links:
            session.delete(link)
        print(f"Deleted {len(orphaned_links)} orphaned session-project links")

        # Delete orphaned event logs
        for log in orphaned_event_logs:
            session.delete(log)
        print(f"Deleted {len(orphaned_event_logs)} orphaned event logs")

        # Delete orphaned drafts
        for draft in orphaned_drafts:
            session.delete(draft)
        print(f"Deleted {len(orphaned_drafts)} orphaned drafts")

        # Delete orphaned messages
        for msg in orphaned_messages:
            session.delete(msg)
        print(f"Deleted {len(orphaned_messages)} orphaned messages")

        # Delete orphaned projects
        for project in orphaned_projects:
            session.delete(project)
        print(f"Deleted {len(orphaned_projects)} orphaned projects")

        # Commit changes
        session.commit()
        print("\n=== Cleanup complete ===")


def main():
    parser = argparse.ArgumentParser(description="Clean up orphaned data")
    parser.add_argument(
        "--execute",
        action="store_true",
        help="Delete confirmed orphaned records; the default is dry-run",
    )
    parser.add_argument(
        "--confirm",
        help="Required with --execute; must equal DELETE_ORPHANED_DATA",
    )
    args = parser.parse_args()

    cleanup_orphaned_data(dry_run=not args.execute, confirmation=args.confirm)


if __name__ == "__main__":
    main()
