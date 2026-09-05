package projects

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("project not found")
	ErrAccessDenied = errors.New("project access denied")
	ErrConflict     = errors.New("project conflict")
)

type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateRequest struct {
	Name        string  `json:"name" vd:"len($)>0 && len($)<=200"`
	Description *string `json:"description,omitempty"`
}

type UpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type Store interface {
	List(context.Context, int64) ([]Project, error)
	Get(context.Context, int64, int64) (Project, error)
	Create(context.Context, int64, CreateRequest) (Project, error)
	Update(context.Context, int64, int64, UpdateRequest) (Project, error)
	Delete(context.Context, int64, int64) error
}
