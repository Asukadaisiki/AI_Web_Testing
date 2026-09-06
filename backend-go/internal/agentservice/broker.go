package agentservice

import "sync"

type Subscription struct {
	Wake   <-chan struct{}
	Cancel func()
}

type EventBroker struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string]map[uint64]chan struct{}
}

func NewEventBroker() *EventBroker {
	return &EventBroker{subscribers: make(map[string]map[uint64]chan struct{})}
}

func (b *EventBroker) Publish(runID string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, channel := range b.subscribers[runID] {
		select {
		case channel <- struct{}{}:
		default:
			// A slow subscriber can recover skipped events from PostgreSQL by seq.
		}
	}
}

func (b *EventBroker) Subscribe(runID string) Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	subscriptionID := b.nextID
	channel := make(chan struct{}, 1)
	if b.subscribers[runID] == nil {
		b.subscribers[runID] = make(map[uint64]chan struct{})
	}
	b.subscribers[runID][subscriptionID] = channel

	var once sync.Once
	return Subscription{
		Wake: channel,
		Cancel: func() {
			once.Do(func() {
				b.mu.Lock()
				defer b.mu.Unlock()
				delete(b.subscribers[runID], subscriptionID)
				if len(b.subscribers[runID]) == 0 {
					delete(b.subscribers, runID)
				}
				close(channel)
			})
		},
	}
}
