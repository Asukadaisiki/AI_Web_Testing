// Package redis provides a Redis-backed disposable live store.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	goredis "github.com/redis/go-redis/v9"
)

// Client stores capped, expiring run live data in Redis streams.
type Client struct {
	client goredis.Cmdable
}

const cancellationCheckInterval = 100 * time.Millisecond

const appendAndExpireScript = `
local id = redis.call("XADD", KEYS[1], "MAXLEN", "~", ARGV[1], "*", "event", ARGV[2])
redis.call("EXPIRE", KEYS[1], ARGV[3])
return id
`

// NewClient binds a live store to a Redis command client.
func NewClient(client goredis.Cmdable) *Client {
	return &Client{client: client}
}

// Append adds a live event to run:{id}:live and refreshes its 24-hour expiry.
func (c *Client) Append(ctx context.Context, runID string, event live.LiveEvent) error {
	if c == nil || c.client == nil {
		return errors.New("redis live store is not configured")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal live event: %w", err)
	}
	if err := c.client.Eval(ctx, appendAndExpireScript, []string{liveKey(runID)},
		live.MaxStreamLength, string(encoded), int64(live.StreamTTL/time.Second)).Err(); err != nil {
		return fmt.Errorf("append live event: %w", err)
	}
	return nil
}

// Read returns live events after afterID, blocking for at most block when requested.
func (c *Client) Read(
	ctx context.Context, runID, afterID string, block time.Duration,
) ([]live.LiveEvent, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("redis live store is not configured")
	}
	if afterID == "" {
		afterID = "0-0"
	}
	if block <= 0 {
		return c.readOnce(ctx, runID, afterID, -1)
	}

	deadline := time.Now().Add(block)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		if remaining > cancellationCheckInterval {
			remaining = cancellationCheckInterval
		}
		events, err := c.readOnce(ctx, runID, afterID, remaining)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
	}
}

func (c *Client) readOnce(
	ctx context.Context, runID, afterID string, block time.Duration,
) ([]live.LiveEvent, error) {
	streams, err := c.client.XRead(ctx, &goredis.XReadArgs{
		Streams: []string{liveKey(runID), afterID},
		Block:   block,
	}).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read live stream: %w", err)
	}

	var events []live.LiveEvent
	for _, stream := range streams {
		for _, message := range stream.Messages {
			encoded, ok := message.Values["event"]
			if !ok {
				return nil, errors.New("live stream message is missing event")
			}
			eventJSON, ok := encoded.(string)
			if !ok {
				return nil, errors.New("live stream event is not a string")
			}
			var event live.LiveEvent
			if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
				return nil, fmt.Errorf("unmarshal live event: %w", err)
			}
			event.ID = message.ID
			events = append(events, event)
		}
	}
	return events, nil
}

func liveKey(runID string) string {
	return "run:" + runID + ":live"
}
