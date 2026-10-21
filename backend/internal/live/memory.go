package live

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// MemoryStore is a context-aware in-memory LiveStore intended for tests.
type MemoryStore struct {
	mu      sync.Mutex
	streams map[string][]LiveEvent
	signals map[string]chan struct{}
	leases  map[string]expiringValue
	cancels map[string]expiringValue
	nextID  int64
}

type expiringValue struct {
	value     string
	expiresAt time.Time
}

// NewMemoryStore creates an empty test live store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams: make(map[string][]LiveEvent),
		signals: make(map[string]chan struct{}),
		leases:  make(map[string]expiringValue),
		cancels: make(map[string]expiringValue),
	}
}

// Append records an event and retains only the configured stream cap.
func (s *MemoryStore) Append(ctx context.Context, runID string, event LiveEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	event.ID = strconv.FormatInt(s.nextID, 10)
	events := append(s.streams[runID], cloneEvent(event))
	if over := int64(len(events)) - MaxStreamLength; over > 0 {
		events = events[over:]
	}
	s.streams[runID] = events
	s.signal(runID)
	return nil
}

// Read returns retained events after afterID, waiting up to block for a new event.
func (s *MemoryStore) Read(ctx context.Context, runID, afterID string, block time.Duration) ([]LiveEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s.mu.Lock()
		events := eventsAfter(s.streams[runID], afterID)
		signal := s.signalFor(runID)
		s.mu.Unlock()
		if len(events) > 0 || block <= 0 {
			return events, nil
		}

		timer := time.NewTimer(block)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-signal:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			return nil, nil
		}
	}
}

// SetLease records a disposable lease for its requested lifetime.
func (s *MemoryStore) SetLease(ctx context.Context, runID, owner string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = LeaseTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leases[runID] = expiringValue{value: owner, expiresAt: time.Now().Add(ttl)}
	return nil
}

// RequestCancel records a disposable cancellation hint for its requested lifetime.
func (s *MemoryStore) RequestCancel(ctx context.Context, runID string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = CancelTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels[runID] = expiringValue{value: "1", expiresAt: time.Now().Add(ttl)}
	return nil
}

// IsCancelRequested reports whether the cancellation hint has not expired.
func (s *MemoryStore) IsCancelRequested(ctx context.Context, runID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.cancels[runID]
	if !ok {
		return false, nil
	}
	if !time.Now().Before(value.expiresAt) {
		delete(s.cancels, runID)
		return false, nil
	}
	return true, nil
}

func (s *MemoryStore) signalFor(runID string) chan struct{} {
	signal := s.signals[runID]
	if signal == nil {
		signal = make(chan struct{}, 1)
		s.signals[runID] = signal
	}
	return signal
}

func (s *MemoryStore) signal(runID string) {
	select {
	case s.signalFor(runID) <- struct{}{}:
	default:
	}
}

func eventsAfter(events []LiveEvent, afterID string) []LiveEvent {
	start := 0
	if afterID != "" && afterID != "0" && afterID != "0-0" {
		for index, event := range events {
			if event.ID == afterID {
				start = index + 1
				break
			}
		}
	}
	result := make([]LiveEvent, len(events)-start)
	for index := start; index < len(events); index++ {
		result[index-start] = cloneEvent(events[index])
	}
	return result
}

func cloneEvent(event LiveEvent) LiveEvent {
	event.Data = append([]byte(nil), event.Data...)
	return event
}
