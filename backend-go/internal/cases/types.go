package cases

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("case not found")
	ErrAccessDenied = errors.New("case access denied")
)

type Mutation struct {
	ProjectID      int64           `json:"project_id" vd:"$>0"`
	Name           string          `json:"name" vd:"len($)>0 && len($)<=200"`
	Description    *string         `json:"description,omitempty"`
	BaseURL        *string         `json:"base_url,omitempty"`
	InputContract  json.RawMessage `json:"input_contract"`
	OutputContract json.RawMessage `json:"output_contract"`
	Steps          json.RawMessage `json:"steps"`
}

type Stored struct {
	ID             int64           `json:"id"`
	ProjectID      int64           `json:"project_id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	BaseURL        *string         `json:"base_url"`
	InputContract  json.RawMessage `json:"input_contract"`
	OutputContract json.RawMessage `json:"output_contract"`
	Steps          json.RawMessage `json:"steps"`
	CreatedBy      int64           `json:"created_by"`
	UpdatedBy      int64           `json:"updated_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Page struct {
	Items      []Stored `json:"items"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalPages int64    `json:"total_pages"`
	HasNext    bool     `json:"has_next"`
	HasPrev    bool     `json:"has_prev"`
}

type Store interface {
	List(context.Context, int64, *int64, string, int, int) (Page, error)
	Get(context.Context, int64, int64) (Stored, error)
	Create(context.Context, int64, Mutation) (Stored, error)
	Update(context.Context, int64, int64, Mutation) (Stored, error)
	Delete(context.Context, int64, int64) error
	DeleteBatch(context.Context, int64, []int64) (int64, error)
}
