// Package kafka implements EventBus with Franz-go.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Config configures a Kafka client.
type Config struct {
	Brokers []string
}

// Client publishes and consumes EventBus messages through Kafka.
type Client struct {
	brokers  []string
	producer *kgo.Client
}

// New creates a Kafka publisher. Consumers get their own client per group through Consume.
func New(config Config) (*Client, error) {
	if len(config.Brokers) == 0 {
		return nil, errors.New("Kafka brokers are required")
	}
	for _, broker := range config.Brokers {
		if strings.TrimSpace(broker) == "" {
			return nil, errors.New("Kafka broker is required")
		}
	}
	producer, err := kgo.NewClient(kgo.SeedBrokers(config.Brokers...))
	if err != nil {
		return nil, fmt.Errorf("create Kafka producer: %w", err)
	}
	return &Client{brokers: append([]string(nil), config.Brokers...), producer: producer}, nil
}

// Publish waits for the broker acknowledgement before returning its position.
func (c *Client) Publish(ctx context.Context, topic, key string, value []byte) (eventbus.Metadata, error) {
	if c == nil || c.producer == nil {
		return eventbus.Metadata{}, errors.New("Kafka client is not configured")
	}
	if strings.TrimSpace(topic) == "" {
		return eventbus.Metadata{}, errors.New("Kafka topic is required")
	}
	record, err := c.producer.ProduceSync(ctx, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	}).First()
	if err != nil {
		return eventbus.Metadata{}, fmt.Errorf("publish Kafka record: %w", err)
	}
	return eventbus.Metadata{Partition: record.Partition, Offset: record.Offset}, nil
}

// Consume disables auto-commit and commits each record only after its handler succeeds.
func (c *Client) Consume(ctx context.Context, topic, group string, handler eventbus.Handler) error {
	if c == nil {
		return errors.New("Kafka client is not configured")
	}
	if strings.TrimSpace(topic) == "" || strings.TrimSpace(group) == "" {
		return errors.New("Kafka topic and consumer group are required")
	}
	if handler == nil {
		return errors.New("Kafka handler is required")
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(c.brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer consumer.Close()

	for {
		fetches := consumer.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("poll Kafka records: %w", err)
		}
		for _, record := range fetches.Records() {
			message := eventbus.Message{
				Topic:     record.Topic,
				Key:       string(record.Key),
				Value:     append([]byte(nil), record.Value...),
				Partition: record.Partition,
				Offset:    record.Offset,
			}
			if err := handler(ctx, message); err != nil {
				return fmt.Errorf("handle Kafka record at %s/%d/%d: %w", record.Topic, record.Partition, record.Offset, err)
			}
			if err := consumer.CommitRecords(ctx, record); err != nil {
				return fmt.Errorf("commit Kafka record at %s/%d/%d: %w", record.Topic, record.Partition, record.Offset, err)
			}
		}
	}
}

// Close releases the publisher's connections.
func (c *Client) Close() error {
	if c != nil && c.producer != nil {
		c.producer.Close()
	}
	return nil
}
