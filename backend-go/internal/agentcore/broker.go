package agentcore

import "sync"

type Subscription struct {
	Events <-chan Event
	Cancel func()
}

type EventBroker struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string]map[uint64]chan Event
}

func NewEventBroker() *EventBroker {
	return &EventBroker{subscribers: make(map[string]map[uint64]chan Event)}
}

func (b *EventBroker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, channel := range b.subscribers[event.RunID] {
		select {
		case channel <- event:
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
	channel := make(chan Event, 64)
	if b.subscribers[runID] == nil {
		b.subscribers[runID] = make(map[uint64]chan Event)
	}
	b.subscribers[runID][subscriptionID] = channel

	var once sync.Once
	return Subscription{
		Events: channel,
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
