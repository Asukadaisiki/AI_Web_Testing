package taskplan

import (
	"context"
	"sync"
)

type MemoryRepository struct {
	mu    sync.RWMutex
	plans map[string][]Plan
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{plans: make(map[string][]Plan)}
}

func (r *MemoryRepository) CreateVersion(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.plans[plan.RunID]
	if len(versions) > 0 {
		versions[len(versions)-1].Status = StatusSuperseded
	}
	plan.Version = len(versions) + 1
	versions = append(versions, clonePlan(plan))
	r.plans[plan.RunID] = versions
	return clonePlan(plan), nil
}

func (r *MemoryRepository) GetCurrent(
	_ context.Context,
	runID string,
) (Plan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.plans[runID]
	if len(versions) == 0 {
		return Plan{}, ErrNotFound
	}
	return clonePlan(versions[len(versions)-1]), nil
}

func (r *MemoryRepository) Save(
	_ context.Context,
	plan Plan,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.plans[plan.RunID]
	for index := range versions {
		if versions[index].ID == plan.ID {
			versions[index] = clonePlan(plan)
			r.plans[plan.RunID] = versions
			return nil
		}
	}
	return ErrNotFound
}

var _ Repository = (*MemoryRepository)(nil)
