package planning

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrSessionNotFound = errors.New("planning session not found")
	ErrProjectNotFound = errors.New("project not found")
	ErrAccessDenied    = errors.New("planning session access denied")
	ErrConflict        = errors.New("planning resource conflict")
)

type CreateSessionRequest struct {
	CaseID    int64 `json:"case_id,omitempty"`
	ProjectID int64 `json:"project_id,omitempty"`
}

type UpdateSessionRequest struct {
	Title  *string `json:"title,omitempty"`
	Status *string `json:"status,omitempty"`
}

type LinkProjectRequest struct {
	ProjectID int64 `json:"project_id" vd:"$>0"`
}

type CreateProjectRequest struct {
	Name        string  `json:"name" vd:"len($)>0 && len($)<=200"`
	Description *string `json:"description,omitempty"`
}

type ProjectSummary struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IsActive    bool    `json:"is_active"`
}

type Session struct {
	ID               int64            `json:"id"`
	ActorUserID      int64            `json:"actor_user_id"`
	RuntimeOwner     string           `json:"runtime_owner"`
	ActiveProjectID  *int64           `json:"active_project_id"`
	CaseID           *int64           `json:"case_id"`
	Title            *string          `json:"title"`
	Status           string           `json:"status"`
	Requirements     json.RawMessage  `json:"requirements"`
	Plan             json.RawMessage  `json:"plan"`
	MissingSlots     json.RawMessage  `json:"missing_slots"`
	LastErrorMessage *string          `json:"last_error_message"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Projects         []ProjectSummary `json:"projects"`
}

type SessionSummary struct {
	ID              int64            `json:"id"`
	RuntimeOwner    string           `json:"runtime_owner"`
	ActiveProjectID *int64           `json:"active_project_id"`
	Title           *string          `json:"title"`
	Status          string           `json:"status"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Projects        []ProjectSummary `json:"projects"`
}

type Message struct {
	ID                int64           `json:"id"`
	SessionID         int64           `json:"session_id"`
	Role              string          `json:"role"`
	TurnType          string          `json:"turn_type"`
	Content           string          `json:"content"`
	StructuredPayload json.RawMessage `json:"structured_payload"`
	CreatedAt         time.Time       `json:"created_at"`
}

type Draft struct {
	ID                 int64           `json:"id"`
	SessionID          int64           `json:"session_id"`
	ScenarioKey        string          `json:"scenario_key"`
	Title              string          `json:"title"`
	Status             string          `json:"status"`
	DSLGenerationID    *int64          `json:"dsl_generation_id"`
	DSLCase            json.RawMessage `json:"dsl_case"`
	Warnings           json.RawMessage `json:"warnings"`
	NormalizationNotes json.RawMessage `json:"normalization_notes"`
	ErrorMessage       *string         `json:"error_message"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type SessionDetail struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages"`
	Drafts   []Draft   `json:"drafts"`
}

type Store interface {
	CreateSession(context.Context, int64, CreateSessionRequest) (SessionDetail, error)
	ListSessions(context.Context, int64) ([]SessionSummary, error)
	GetSession(context.Context, int64, int64) (SessionDetail, error)
	UpdateSession(context.Context, int64, int64, UpdateSessionRequest) (SessionDetail, error)
	DeleteSession(context.Context, int64, int64) error
	ListProjects(context.Context, int64, int64) ([]ProjectSummary, error)
	LinkProject(context.Context, int64, int64, int64) (ProjectSummary, error)
	UnlinkProject(context.Context, int64, int64, int64) error
	CreateProject(context.Context, int64, int64, CreateProjectRequest) (ProjectSummary, error)
	ResolveRunContext(context.Context, int64, int64) (string, int64, error)
}
