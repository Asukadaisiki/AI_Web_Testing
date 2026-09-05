package agentservice

import (
	"testing"
	"time"
)

func TestEventBrokerPublishesByRun(t *testing.T) {
	broker := NewEventBroker()
	subscription := broker.Subscribe("run-1")
	defer subscription.Cancel()

	broker.Publish(Event{RunID: "run-2", Seq: 1})
	broker.Publish(Event{RunID: "run-1", Seq: 2})

	select {
	case event := <-subscription.Events:
		if event.RunID != "run-1" || event.Seq != 2 {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBrokerDropsForSlowSubscriber(t *testing.T) {
	broker := NewEventBroker()
	subscription := broker.Subscribe("run-1")
	defer subscription.Cancel()

	for sequence := int64(1); sequence <= 100; sequence++ {
		broker.Publish(Event{RunID: "run-1", Seq: sequence})
	}
}
