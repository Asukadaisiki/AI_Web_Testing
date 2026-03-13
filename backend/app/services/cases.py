"""Test case persistence services."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import Project, TestCase, User
from app.schemas.cases import CaseCreateRequest, CaseUpdateRequest, StoredCaseDetail, StoredCaseSummary
from app.schemas.dsl import DSLCase


class EntityNotFoundError(ValueError):
    """Raised when a required entity does not exist."""


def create_case(session: Session, payload: CaseCreateRequest) -> StoredCaseDetail:
    _ensure_project_exists(session, payload.project_id)
    _ensure_user_exists(session, payload.actor_user_id)

    case = TestCase(
        project_id=payload.project_id,
        created_by=payload.actor_user_id,
        updated_by=payload.actor_user_id,
        name=payload.name,
        description=payload.description,
        dsl=payload.model_dump(mode="json", exclude={"project_id", "actor_user_id"}),
    )
    session.add(case)
    session.commit()
    session.refresh(case)
    return _to_stored_case_detail(case)


def update_case(session: Session, case_id: int, payload: CaseUpdateRequest) -> StoredCaseDetail:
    case = session.get(TestCase, case_id)
    if case is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")

    _ensure_project_exists(session, payload.project_id)
    _ensure_user_exists(session, payload.actor_user_id)

    case.project_id = payload.project_id
    case.updated_by = payload.actor_user_id
    case.name = payload.name
    case.description = payload.description
    case.dsl = payload.model_dump(mode="json", exclude={"project_id", "actor_user_id"})
    session.add(case)
    session.commit()
    session.refresh(case)
    return _to_stored_case_detail(case)


def list_cases(session: Session) -> list[StoredCaseSummary]:
    statement = select(TestCase).order_by(TestCase.created_at.desc(), TestCase.id.desc())
    records = session.scalars(statement).all()
    return [_to_stored_case_summary(record) for record in records]


def get_case(session: Session, case_id: int) -> StoredCaseDetail | None:
    record = session.get(TestCase, case_id)
    if record is None:
        return None
    return _to_stored_case_detail(record)


def _ensure_project_exists(session: Session, project_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _to_stored_case_summary(record: TestCase) -> StoredCaseSummary:
    normalized_case = DSLCase.model_validate(record.dsl)
    return StoredCaseSummary(
        id=record.id,
        project_id=record.project_id,
        name=record.name,
        description=record.description,
        base_url=normalized_case.base_url,
        input_contract=normalized_case.input_contract,
        output_contract=normalized_case.output_contract,
        steps=normalized_case.steps,
        created_by=record.created_by,
        updated_by=record.updated_by,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )


def _to_stored_case_detail(record: TestCase) -> StoredCaseDetail:
    summary = _to_stored_case_summary(record)
    return StoredCaseDetail(**summary.model_dump())
