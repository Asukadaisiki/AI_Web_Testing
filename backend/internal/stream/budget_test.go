package stream

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubscriptionBudgetAndSlowConsumerReleaseResources(t *testing.T) {
	service := NewService(&memoryDurableReader{events: []DurableEvent{{Sequence: 1, Type: "event"}}}, nil,
		Config{MaxSubscriptions: 1, SendTimeout: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, firstErrors := service.Subscribe(ctx, "run", "")
	_, secondErrors := service.Subscribe(ctx, "other", "")
	if err := <-secondErrors; !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("budget: %v", err)
	}
	if err := <-firstErrors; !errors.Is(err, ErrSlowConsumer) {
		t.Fatalf("slow receiver: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for service.Stats().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if service.Stats().Active != 0 || service.Stats().Rejected != 1 || service.Stats().SlowConsumers != 1 {
		t.Fatalf("resources: %+v", service.Stats())
	}
}
