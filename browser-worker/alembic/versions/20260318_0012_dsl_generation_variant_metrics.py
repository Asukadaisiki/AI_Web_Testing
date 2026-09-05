"""Add prompt variant and risk flag fields to AI DSL generation runs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260318_0012"
down_revision = "20260318_0011"
branch_labels = None
depends_on = None


def _derive_prompt_variant(row: sa.RowMapping) -> str:
    # This mirrors app.ai.dsl_generator.resolve_generation_profile() as of 2026-03-18.
    # The logic is intentionally frozen inside the migration so historical backfill stays deterministic.
    if row["import_mode"] == "contracts_only":
        return "contracts_focus"
    if row["generation_mode"] == "strict_steps_only" and row["used_current_steps_context"]:
        return "repair_steps"
    if row["used_current_case_context"]:
        return "rewrite_from_case"
    return "baseline_draft"


def _derive_context_profile(row: sa.RowMapping) -> str:
    # This mirrors app.ai.dsl_generator.resolve_generation_profile() as of 2026-03-18.
    # Keep it local to the migration instead of importing app code, so future rule changes do not rewrite history.
    if row["import_mode"] == "contracts_only":
        return "contracts_focus"
    if row["generation_mode"] == "strict_steps_only" and row["used_current_steps_context"]:
        return "repair_steps"
    if row["used_current_case_context"]:
        return "rewrite_from_case"
    return "blank_request"


def _derive_risk_flags(row: sa.RowMapping) -> list[str]:
    risk_flags: list[str] = []
    # Historical rows can only be reconstructed from columns that already existed before this migration.
    # missing_name_fallback was not stored previously, so it is intentionally not backfilled here.
    if row["base_url_backfilled"]:
        risk_flags.append("base_url_backfilled")
    if (row["repaired_invalid_actions"] or 0) > 0:
        risk_flags.append("invalid_actions_repaired")
    if (row["removed_invalid_steps"] or 0) > 0:
        risk_flags.append("invalid_steps_removed")
    if (row["removed_invalid_contracts"] or 0) > 0:
        risk_flags.append("invalid_contracts_removed")
    if row["preserve_contracts_applied"]:
        risk_flags.append("contracts_preserved_fallback")
    return risk_flags


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(sa.Column("prompt_variant", sa.String(length=32), nullable=True))
        batch_op.add_column(sa.Column("context_profile", sa.String(length=32), nullable=True))
        batch_op.add_column(
            sa.Column(
                "risk_flags_json",
                sa.JSON(),
                nullable=False,
                server_default=sa.text("'[]'"),
            )
        )

    bind = op.get_bind()
    generation_runs = sa.table(
        "dsl_generation_runs",
        sa.column("id", sa.Integer()),
        sa.column("import_mode", sa.String()),
        sa.column("generation_mode", sa.String()),
        sa.column("used_current_case_context", sa.Boolean()),
        sa.column("used_current_steps_context", sa.Boolean()),
        sa.column("base_url_backfilled", sa.Boolean()),
        sa.column("repaired_invalid_actions", sa.Integer()),
        sa.column("removed_invalid_steps", sa.Integer()),
        sa.column("removed_invalid_contracts", sa.Integer()),
        sa.column("preserve_contracts_applied", sa.Boolean()),
        sa.column("prompt_variant", sa.String()),
        sa.column("context_profile", sa.String()),
        sa.column("risk_flags_json", sa.JSON()),
    )
    rows = bind.execute(
        sa.select(
            generation_runs.c.id,
            generation_runs.c.import_mode,
            generation_runs.c.generation_mode,
            generation_runs.c.used_current_case_context,
            generation_runs.c.used_current_steps_context,
            generation_runs.c.base_url_backfilled,
            generation_runs.c.repaired_invalid_actions,
            generation_runs.c.removed_invalid_steps,
            generation_runs.c.removed_invalid_contracts,
            generation_runs.c.preserve_contracts_applied,
        )
    ).mappings()

    for row in rows:
        bind.execute(
            generation_runs.update()
            .where(generation_runs.c.id == row["id"])
            .values(
                prompt_variant=_derive_prompt_variant(row),
                context_profile=_derive_context_profile(row),
                risk_flags_json=_derive_risk_flags(row),
            )
        )

    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_prompt_variant",
            "prompt_variant IN ('baseline_draft', 'rewrite_from_case', 'repair_steps', 'contracts_focus')",
        )
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_context_profile",
            "context_profile IN ('blank_request', 'rewrite_from_case', 'repair_steps', 'contracts_focus')",
        )
        batch_op.alter_column("prompt_variant", nullable=False)
        batch_op.alter_column("context_profile", nullable=False)
        batch_op.alter_column("risk_flags_json", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_constraint("ck_dsl_generation_runs_context_profile", type_="check")
        batch_op.drop_constraint("ck_dsl_generation_runs_prompt_variant", type_="check")
        batch_op.drop_column("risk_flags_json")
        batch_op.drop_column("context_profile")
        batch_op.drop_column("prompt_variant")
