package serviceprobe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type requestFunc func(context.Context, kmsg.Request) (kmsg.Response, error)

func (f requestFunc) Request(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	return f(ctx, req)
}

func TestKafkaLagUsesOnlyTrustedReadRequestsAndCompleteOffsets(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		committed              int64
		code                   int16
		omit                   bool
		modern                 bool
		wantStatus, wantReason string
	}{
		{"measured", 6, 0, false, false, "measured", ""},
		{"modern_measured", 6, 0, false, true, "measured", ""},
		{"no_commit", -1, 0, false, false, "unmeasured", "offset_not_committed"},
		{"partition_error", 6, 30, false, false, "unmeasured", "offset_fetch_failed"},
		{"partial_response", 6, 0, true, false, "unmeasured", "incomplete_offsets"},
		{"ahead_of_end", 12, 0, false, false, "unmeasured", "inconsistent_offsets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			probe := &KafkaLag{topic: "trusted.commands", group: "trusted.runtime", requestor: requestFunc(func(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
				requests++
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2100*time.Millisecond {
					t.Fatal("probe must have a bounded deadline")
				}
				switch r := req.(type) {
				case *kmsg.MetadataRequest:
					if r.AllowAutoTopicCreation || len(r.Topics) != 1 || r.Topics[0].Topic == nil || *r.Topics[0].Topic != "trusted.commands" {
						t.Fatalf("unsafe metadata request: %+v", r)
					}
					topic := "trusted.commands"
					return &kmsg.MetadataResponse{Topics: []kmsg.MetadataResponseTopic{{Topic: &topic, Partitions: []kmsg.MetadataResponseTopicPartition{{Partition: 0}, {Partition: 1}}}}}, nil
				case *kmsg.OffsetFetchRequest:
					if r.Group != "trusted.runtime" || len(r.Topics) != 1 || r.Topics[0].Topic != "trusted.commands" ||
						len(r.Groups) != 1 || r.Groups[0].Group != r.Group || len(r.Groups[0].Topics) != 1 {
						t.Fatalf("unsafe offset fetch request: %+v", r)
					}
					parts := []kmsg.OffsetFetchResponseTopicPartition{{Partition: 0, Offset: tc.committed, ErrorCode: tc.code}}
					if !tc.omit {
						parts = append(parts, kmsg.OffsetFetchResponseTopicPartition{Partition: 1, Offset: 7})
					}
					if tc.modern {
						return &kmsg.OffsetFetchResponse{Groups: []kmsg.OffsetFetchResponseGroup{{Group: "trusted.runtime", Topics: []kmsg.OffsetFetchResponseGroupTopic{{Topic: "trusted.commands", Partitions: []kmsg.OffsetFetchResponseGroupTopicPartition{
							{Partition: 0, Offset: tc.committed}, {Partition: 1, Offset: 7},
						}}}}}}, nil
					}
					return &kmsg.OffsetFetchResponse{Topics: []kmsg.OffsetFetchResponseTopic{{Topic: "trusted.commands", Partitions: parts}}}, nil
				case *kmsg.ListOffsetsRequest:
					if len(r.Topics) != 1 || r.Topics[0].Topic != "trusted.commands" || len(r.Topics[0].Partitions) != 2 {
						t.Fatalf("unsafe list offsets request: %+v", r)
					}
					for _, part := range r.Topics[0].Partitions {
						if part.Timestamp != -1 {
							t.Fatal("list offsets must only read log end offsets")
						}
					}
					return &kmsg.ListOffsetsResponse{Topics: []kmsg.ListOffsetsResponseTopic{{Topic: "trusted.commands", Partitions: []kmsg.ListOffsetsResponseTopicPartition{{Partition: 0, Offset: 10}, {Partition: 1, Offset: 10}}}}}, nil
				default:
					t.Fatalf("mutating/unexpected Kafka request: %T", r)
					return nil, errors.New("unexpected request")
				}
			})}
			got := probe.Measure(context.Background())
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason || got.Scope != "deployment_consumer_group" ||
				got.ConsumerGroup != "trusted.runtime" || got.Topic != "trusted.commands" {
				t.Fatalf("measurement=%+v", got)
			}
			if tc.wantStatus == "measured" {
				if got.Value == nil || *got.Value != 7 || requests != 3 {
					t.Fatalf("expected total lag 7 from three reads, got %+v requests=%d", got, requests)
				}
			} else if got.Value != nil {
				t.Fatalf("failed measurements must not be zero: %+v", got)
			}
		})
	}
}

func TestKafkaLagHonorsEarlierCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &KafkaLag{topic: "commands", group: "runtime", requestor: requestFunc(func(ctx context.Context, _ kmsg.Request) (kmsg.Response, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("probe discarded caller cancellation")
		}
		return nil, ctx.Err()
	})}
	if got := p.Measure(ctx); got.Status != "unmeasured" || got.Value != nil {
		t.Fatalf("canceled probe fabricated lag: %+v", got)
	}
}

func TestKafkaLagRedactsTransportFailure(t *testing.T) {
	p := &KafkaLag{topic: "commands", group: "runtime", requestor: requestFunc(func(context.Context, kmsg.Request) (kmsg.Response, error) {
		return nil, errors.New("kafka://user:password@private.invalid")
	})}
	got := p.Measure(context.Background())
	raw, _ := json.Marshal(got)
	if got.Value != nil || got.Status != "unmeasured" || got.Reason != "metadata_unavailable" || strings.Contains(string(raw), "private.invalid") {
		t.Fatalf("unsafe failure measurement: %s", raw)
	}
}

func TestKafkaLagRequiresTrustedConfigAndNilIsUnknown(t *testing.T) {
	if _, err := NewKafkaLag(config.Kafka{}); err == nil {
		t.Fatal("missing deployment config accepted")
	}
	probe, err := NewKafkaLag(config.Kafka{Brokers: []string{"127.0.0.1:1"}, CommandTopic: "trusted.commands", GroupID: "trusted.runtime"})
	if err != nil {
		t.Fatal(err)
	}
	probe.Close()
	var p *KafkaLag
	if got := p.Measure(context.Background()); got.Status != "unmeasured" || got.Value != nil || got.Reason != "probe_not_configured" {
		t.Fatalf("nil probe falsely measured: %+v", got)
	}
}
