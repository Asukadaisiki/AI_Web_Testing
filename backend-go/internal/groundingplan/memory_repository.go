package groundingplan

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu    sync.RWMutex
	plans map[string][]Plan
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{plans: make(map[string][]Plan)}
}

func (r *MemoryRepository) CreateInitial(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	revisions := r.plans[plan.TaskPlanID]
	if len(revisions) > 0 {
		return clonePlan(revisions[len(revisions)-1]), nil
	}
	if plan.Revision != 1 {
		return Plan{}, ErrConflict
	}
	r.plans[plan.TaskPlanID] = []Plan{clonePlan(plan)}
	return clonePlan(plan), nil
}

func (r *MemoryRepository) ReplaceForTaskPlan(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if plan.Revision != 1 {
		return Plan{}, ErrConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if revisions := r.plans[plan.TaskPlanID]; len(revisions) > 0 {
		return clonePlan(revisions[len(revisions)-1]), nil
	}

	previousTaskPlanID := ""
	var previous Plan
	for taskPlanID, revisions := range r.plans {
		if len(revisions) == 0 {
			continue
		}
		candidate := revisions[len(revisions)-1]
		if candidate.RunID != plan.RunID {
			continue
		}
		if previousTaskPlanID == "" ||
			candidate.TaskPlanVersion > previous.TaskPlanVersion ||
			(candidate.TaskPlanVersion == previous.TaskPlanVersion &&
				candidate.Revision > previous.Revision) {
			previousTaskPlanID = taskPlanID
			previous = candidate
		}
	}
	if previousTaskPlanID != "" {
		if previous.TaskPlanVersion > plan.TaskPlanVersion {
			return Plan{}, ErrConflict
		}
		if previous.Status != StatusSuperseded {
			superseded, err := supersededRevision(previous, plan.UpdatedAt)
			if err != nil {
				return Plan{}, err
			}
			r.plans[previousTaskPlanID] = append(
				r.plans[previousTaskPlanID],
				clonePlan(superseded),
			)
		}
	}
	r.plans[plan.TaskPlanID] = []Plan{clonePlan(plan)}
	return clonePlan(plan), nil
}

func (r *MemoryRepository) AppendRevision(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	revisions := r.plans[plan.TaskPlanID]
	if len(revisions) == 0 {
		return Plan{}, ErrNotFound
	}
	current := revisions[len(revisions)-1]
	if plan.Revision != current.Revision+1 ||
		plan.TaskPlanBinding() != current.TaskPlanBinding() ||
		plan.RunID != current.RunID {
		return Plan{}, ErrConflict
	}
	r.plans[plan.TaskPlanID] = append(revisions, clonePlan(plan))
	return clonePlan(plan), nil
}

func (r *MemoryRepository) GetCurrent(
	_ context.Context,
	key string,
) (Plan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if revisions := r.plans[key]; len(revisions) > 0 {
		return clonePlan(revisions[len(revisions)-1]), nil
	}
	var current *Plan
	for _, revisions := range r.plans {
		if len(revisions) == 0 {
			continue
		}
		candidate := revisions[len(revisions)-1]
		if candidate.RunID != key {
			continue
		}
		if current == nil ||
			candidate.TaskPlanVersion > current.TaskPlanVersion ||
			(candidate.TaskPlanVersion == current.TaskPlanVersion &&
				candidate.Revision > current.Revision) {
			copy := clonePlan(candidate)
			current = &copy
		}
	}
	if current == nil {
		return Plan{}, ErrNotFound
	}
	return *current, nil
}

func (r *MemoryRepository) SupersedeForTaskPlan(
	_ context.Context,
	taskPlanID string,
	at time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	revisions := r.plans[taskPlanID]
	if len(revisions) == 0 {
		return nil
	}
	current := revisions[len(revisions)-1]
	if current.Status == StatusSuperseded {
		return nil
	}
	current, err := supersededRevision(current, at)
	if err != nil {
		return err
	}
	r.plans[taskPlanID] = append(revisions, clonePlan(current))
	return nil
}

var _ Repository = (*MemoryRepository)(nil)
