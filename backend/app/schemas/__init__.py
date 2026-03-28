"""Schemas package."""

from app.schemas.cases import CaseCreateRequest, StoredCaseDetail, StoredCaseSummary
from app.schemas.dsl import DSLCase, DSLStep, DSLValidationResult
from app.schemas.executions import (
    CaseExecutionRequest,
    ExecutionReport,
    StepExecutionEvidence,
    StoredCaseExecutionDetail,
    StoredCaseExecutionSummary,
)
from app.schemas.projects import ProjectSummary
from app.schemas.reports import ReportPreferencePayload
from app.schemas.suites import (
    StoredSuiteDetail,
    StoredSuiteSummary,
    SuiteCreateRequest,
    SuiteExecutionRequest,
    SuiteExecutionResult,
    SuiteUpdateRequest,
)

__all__ = [
    "CaseCreateRequest",
    "CaseExecutionRequest",
    "DSLCase",
    "DSLStep",
    "DSLValidationResult",
    "ExecutionReport",
    "ProjectSummary",
    "ReportPreferencePayload",
    "StepExecutionEvidence",
    "StoredCaseExecutionDetail",
    "StoredCaseExecutionSummary",
    "StoredCaseDetail",
    "StoredCaseSummary",
    "StoredSuiteDetail",
    "StoredSuiteSummary",
    "SuiteCreateRequest",
    "SuiteExecutionRequest",
    "SuiteExecutionResult",
    "SuiteUpdateRequest",
]
