"""Services package."""

from app.services.cases import (
    EntityNotFoundError,
    batch_delete_cases,
    batch_update_cases,
    create_case,
    delete_case,
    get_case,
    list_cases,
    list_cases_paginated,
    update_case,
)
from app.services.corrections import (
    CorrectionConflictError,
    batch_update_correction_state,
    create_correction,
    get_corrections_overview,
    list_corrections,
    list_correction_events,
    update_correction_state,
)
from app.services.dsl import SUPPORTED_DSL_ACTIONS, validate_dsl_case
from app.services.executions import (
    execute_case,
    get_executions_overview,
    get_case_execution,
    list_case_executions,
    list_executions,
)
from app.services.projects import list_accessible_projects
from app.services.project_management import (
    create_project,
    delete_project,
    get_project,
    list_projects,
    ProjectAccessError,
    update_project,
)
from app.services.report_preferences import get_report_preference, update_report_preference

__all__ = [
    "CorrectionConflictError",
    "EntityNotFoundError",
    "SUPPORTED_DSL_ACTIONS",
    "batch_delete_cases",
    "batch_update_correction_state",
    "batch_update_cases",
    "create_case",
    "create_correction",
    "create_project",
    "delete_case",
    "delete_project",
    "execute_case",
    "get_case",
    "get_case_execution",
    "get_corrections_overview",
    "get_executions_overview",
    "get_project",
    "get_report_preference",
    "list_accessible_projects",
    "list_case_executions",
    "list_cases",
    "list_cases_paginated",
    "list_correction_events",
    "list_corrections",
    "list_executions",
    "list_projects",
    "ProjectAccessError",
    "update_case",
    "update_correction_state",
    "update_project",
    "update_report_preference",
    "validate_dsl_case",
]
