// Package eventbus defines the broker boundary used by durable delivery components.
package eventbus

import "context"

// Metadata identifies the Kafka position assigned to a published message.
type Metadata struct {
	Partition int32
	Offset    int64
}

// Message is a broker-delivered record.
type Message struct {
	Topic     string
	Key       string
	Value     []byte
	Partition int32
	Offset    int64
}

// Handler processes one broker-delivered message.
type Handler func(context.Context, Message) error

// EventBus publishes and consumes durable event records.
type EventBus interface {
	Publish(ctx context.Context, topic, key string, value []byte) (Metadata, error)
	Consume(ctx context.Context, topic, group string, handler Handler) error
	Close() error
}
