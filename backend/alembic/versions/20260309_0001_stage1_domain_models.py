"""Create stage 1 domain models."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260309_0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "users",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("email", sa.String(length=255), nullable=False),
        sa.Column("display_name", sa.String(length=100), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_users")),
    )
    op.create_index(op.f("ix_users_email"), "users", ["email"], unique=True)

    op.create_table(
        "projects",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("name", sa.String(length=200), nullable=False),
        sa.Column("description", sa.String(length=1000), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_projects")),
    )
    op.create_index(op.f("ix_projects_name"), "projects", ["name"], unique=True)

    op.create_table(
        "project_members",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("user_id", sa.Integer(), nullable=False),
        sa.Column("role", sa.String(length=50), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_project_members_project_id_projects"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["user_id"],
            ["users.id"],
            name=op.f("fk_project_members_user_id_users"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_project_members")),
        sa.UniqueConstraint("project_id", "user_id", name=op.f("uq_project_members_project_id")),
    )
    op.create_index(op.f("ix_project_members_project_id"), "project_members", ["project_id"], unique=False)
    op.create_index(op.f("ix_project_members_user_id"), "project_members", ["user_id"], unique=False)

    op.create_table(
        "test_cases",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("created_by", sa.Integer(), nullable=False),
        sa.Column("updated_by", sa.Integer(), nullable=False),
        sa.Column("name", sa.String(length=200), nullable=False),
        sa.Column("description", sa.String(length=1000), nullable=True),
        sa.Column("dsl", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(
            ["created_by"],
            ["users.id"],
            name=op.f("fk_test_cases_created_by_users"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_test_cases_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["updated_by"],
            ["users.id"],
            name=op.f("fk_test_cases_updated_by_users"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_test_cases")),
    )
    op.create_index(op.f("ix_test_cases_created_by"), "test_cases", ["created_by"], unique=False)
    op.create_index(op.f("ix_test_cases_name"), "test_cases", ["name"], unique=False)
    op.create_index(op.f("ix_test_cases_project_id"), "test_cases", ["project_id"], unique=False)
    op.create_index(op.f("ix_test_cases_updated_by"), "test_cases", ["updated_by"], unique=False)

    op.create_table(
        "test_suites",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("created_by", sa.Integer(), nullable=False),
        sa.Column("updated_by", sa.Integer(), nullable=False),
        sa.Column("name", sa.String(length=200), nullable=False),
        sa.Column("description", sa.String(length=1000), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(
            ["created_by"],
            ["users.id"],
            name=op.f("fk_test_suites_created_by_users"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_test_suites_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["updated_by"],
            ["users.id"],
            name=op.f("fk_test_suites_updated_by_users"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_test_suites")),
    )
    op.create_index(op.f("ix_test_suites_created_by"), "test_suites", ["created_by"], unique=False)
    op.create_index(op.f("ix_test_suites_name"), "test_suites", ["name"], unique=False)
    op.create_index(op.f("ix_test_suites_project_id"), "test_suites", ["project_id"], unique=False)
    op.create_index(op.f("ix_test_suites_updated_by"), "test_suites", ["updated_by"], unique=False)

    op.create_table(
        "suite_cases",
        sa.Column("suite_id", sa.Integer(), nullable=False),
        sa.Column("case_id", sa.Integer(), nullable=False),
        sa.Column("order_index", sa.Integer(), nullable=False),
        sa.ForeignKeyConstraint(
            ["case_id"],
            ["test_cases.id"],
            name=op.f("fk_suite_cases_case_id_test_cases"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["suite_id"],
            ["test_suites.id"],
            name=op.f("fk_suite_cases_suite_id_test_suites"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("suite_id", "case_id", name=op.f("pk_suite_cases")),
        sa.UniqueConstraint("suite_id", "order_index", name=op.f("uq_suite_cases_suite_id")),
    )

    users_table = sa.table(
        "users",
        sa.column("id", sa.Integer()),
        sa.column("email", sa.String(length=255)),
        sa.column("display_name", sa.String(length=100)),
    )
    projects_table = sa.table(
        "projects",
        sa.column("id", sa.Integer()),
        sa.column("name", sa.String(length=200)),
        sa.column("description", sa.String(length=1000)),
    )
    project_members_table = sa.table(
        "project_members",
        sa.column("id", sa.Integer()),
        sa.column("project_id", sa.Integer()),
        sa.column("user_id", sa.Integer()),
        sa.column("role", sa.String(length=50)),
    )

    op.bulk_insert(
        users_table,
        [
            {
                "id": 1,
                "email": "seed-owner@example.com",
                "display_name": "Seed Owner",
            }
        ],
    )
    op.bulk_insert(
        projects_table,
        [
            {
                "id": 1,
                "name": "Default Project",
                "description": "Seed project for local development and tests.",
            }
        ],
    )
    op.bulk_insert(
        project_members_table,
        [
            {
                "id": 1,
                "project_id": 1,
                "user_id": 1,
                "role": "owner",
            }
        ],
    )


def downgrade() -> None:
    op.drop_table("suite_cases")
    op.drop_index(op.f("ix_test_suites_updated_by"), table_name="test_suites")
    op.drop_index(op.f("ix_test_suites_project_id"), table_name="test_suites")
    op.drop_index(op.f("ix_test_suites_name"), table_name="test_suites")
    op.drop_index(op.f("ix_test_suites_created_by"), table_name="test_suites")
    op.drop_table("test_suites")
    op.drop_index(op.f("ix_test_cases_updated_by"), table_name="test_cases")
    op.drop_index(op.f("ix_test_cases_project_id"), table_name="test_cases")
    op.drop_index(op.f("ix_test_cases_name"), table_name="test_cases")
    op.drop_index(op.f("ix_test_cases_created_by"), table_name="test_cases")
    op.drop_table("test_cases")
    op.drop_index(op.f("ix_project_members_user_id"), table_name="project_members")
    op.drop_index(op.f("ix_project_members_project_id"), table_name="project_members")
    op.drop_table("project_members")
    op.drop_index(op.f("ix_projects_name"), table_name="projects")
    op.drop_table("projects")
    op.drop_index(op.f("ix_users_email"), table_name="users")
    op.drop_table("users")
