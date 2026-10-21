package eventbus

import (
	"context"
	"sync"
)

// Fake is an in-memory EventBus for deterministic tests.
type Fake struct {
	mu           sync.Mutex
	NextMetadata Metadata
	PublishErr   error
	ConsumeErr   error
	published    []Message
}

// NewFake creates an empty in-memory bus.
func NewFake() *Fake {
	return &Fake{}
}

// Publish records a message or returns the configured error.
func (f *Fake) Publish(_ context.Context, topic, key string, value []byte) (Metadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.published = append(f.published, Message{
		Topic: topic,
		Key:   key,
		Value: append([]byte(nil), value...),
	})
	if f.PublishErr != nil {
		return Metadata{}, f.PublishErr
	}
	return f.NextMetadata, nil
}

// Consume returns the configured error. Tests invoke handlers directly.
func (f *Fake) Consume(_ context.Context, _ string, _ string, _ Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ConsumeErr
}

// Close releases no resources.
func (f *Fake) Close() error {
	return nil
}

// Published returns a snapshot of publish attempts.
func (f *Fake) Published() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()

	messages := make([]Message, len(f.published))
	for i, message := range f.published {
		messages[i] = Message{
			Topic:     message.Topic,
			Key:       message.Key,
			Value:     append([]byte(nil), message.Value...),
			Partition: message.Partition,
			Offset:    message.Offset,
		}
	}
	return messages
}
