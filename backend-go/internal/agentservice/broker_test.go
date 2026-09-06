package agentservice

import (
	"testing"
	"time"
)

func TestEventBrokerPublishesByRun(t *testing.T) {
	broker := NewEventBroker()
	subscription := broker.Subscribe("run-1")
	defer subscription.Cancel()

	broker.Publish("run-2")
	broker.Publish("run-1")

	select {
	case <-subscription.Wake:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBrokerCoalescesWakeupsForSlowSubscriber(t *testing.T) {
	broker := NewEventBroker()
	subscription := broker.Subscribe("run-1")
	defer subscription.Cancel()

	for range 100 {
		broker.Publish("run-1")
	}
}
