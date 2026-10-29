// Package serviceprobe provides bounded, read-only deployment observations.
package serviceprobe

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type Measurement struct {
	Status        string     `json:"status"`
	Scope         string     `json:"scope"`
	Reason        string     `json:"reason,omitempty"`
	Value         *float64   `json:"value"`
	ConsumerGroup string     `json:"consumer_group,omitempty"`
	Topic         string     `json:"topic,omitempty"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
}

type KafkaLag struct {
	requestor kmsg.Requestor
	client    *kgo.Client
	topic     string
	group     string
}

// NewKafkaLag accepts deployment configuration only. It never joins a consumer
// group, consumes records, commits offsets, or enables topic creation.
func NewKafkaLag(cfg config.Kafka) (*KafkaLag, error) {
	if len(cfg.Brokers) == 0 || strings.TrimSpace(cfg.CommandTopic) == "" || strings.TrimSpace(cfg.GroupID) == "" {
		return nil, errors.New("Kafka lag requires deployment brokers, command topic and runtime group")
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.DialTimeout(time.Second),
		kgo.RequestRetries(0),
	)
	if err != nil {
		return nil, errors.New("Kafka lag client configuration failed")
	}
	return &KafkaLag{requestor: client, client: client, topic: cfg.CommandTopic, group: cfg.GroupID}, nil
}

func (p *KafkaLag) Close() {
	if p != nil && p.client != nil {
		p.client.Close()
	}
}

// Measure is lag for the whole configured runtime group, not project lag.
// Missing commits and incomplete/error responses remain unknown, never zero.
func (p *KafkaLag) Measure(ctx context.Context) Measurement {
	result := Measurement{Status: "unmeasured", Scope: "deployment_consumer_group", Reason: "probe_not_configured"}
	if p == nil || p.requestor == nil {
		return result
	}
	result.ConsumerGroup, result.Topic = p.group, p.topic
	fail := func(reason string) Measurement {
		result.Reason = reason
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	meta := kmsg.NewPtrMetadataRequest()
	meta.AllowAutoTopicCreation = false
	meta.Topics = []kmsg.MetadataRequestTopic{{Topic: &p.topic}}
	metadata, err := meta.RequestWith(ctx, p.requestor)
	if err != nil || metadata == nil {
		return fail("metadata_unavailable")
	}
	if len(metadata.Topics) != 1 || metadata.Topics[0].Topic == nil || *metadata.Topics[0].Topic != p.topic ||
		metadata.Topics[0].ErrorCode != 0 {
		return fail("topic_metadata_unavailable")
	}
	partitions := metadata.Topics[0].Partitions
	if len(partitions) == 0 || len(partitions) > 256 {
		return fail("partition_count_unsupported")
	}
	wanted := make(map[int32]bool, len(partitions))
	ids := make([]int32, 0, len(partitions))
	for _, partition := range partitions {
		if partition.ErrorCode != 0 || partition.Partition < 0 || wanted[partition.Partition] {
			return fail("partition_metadata_unavailable")
		}
		wanted[partition.Partition] = true
		ids = append(ids, partition.Partition)
	}
	fetch := kmsg.NewPtrOffsetFetchRequest()
	// Franz-go negotiates versions; populate the legacy and v8+ layouts.
	fetch.Group = p.group
	fetch.Topics = []kmsg.OffsetFetchRequestTopic{{Topic: p.topic, Partitions: ids}}
	group := kmsg.NewOffsetFetchRequestGroup()
	group.Group = p.group
	group.Topics = []kmsg.OffsetFetchRequestGroupTopic{{Topic: p.topic, Partitions: ids}}
	fetch.Groups = []kmsg.OffsetFetchRequestGroup{group}
	committed, err := fetch.RequestWith(ctx, p.requestor)
	if err != nil || committed == nil || committed.ErrorCode != 0 {
		return fail("offset_fetch_failed")
	}
	offsets := make(map[int32]int64, len(ids))
	add := func(topic string, partition int32, offset int64, code int16) string {
		if topic != p.topic || !wanted[partition] {
			return "incomplete_offsets"
		}
		if _, exists := offsets[partition]; exists {
			return "incomplete_offsets"
		}
		if code != 0 {
			return "offset_fetch_failed"
		}
		if offset < 0 {
			return "offset_not_committed"
		}
		offsets[partition] = offset
		return ""
	}
	if len(committed.Groups) > 0 {
		if len(committed.Groups) != 1 || committed.Groups[0].Group != p.group || committed.Groups[0].ErrorCode != 0 {
			return fail("offset_fetch_failed")
		}
		for _, topic := range committed.Groups[0].Topics {
			for _, partition := range topic.Partitions {
				if reason := add(topic.Topic, partition.Partition, partition.Offset, partition.ErrorCode); reason != "" {
					return fail(reason)
				}
			}
		}
	} else {
		for _, topic := range committed.Topics {
			for _, partition := range topic.Partitions {
				if reason := add(topic.Topic, partition.Partition, partition.Offset, partition.ErrorCode); reason != "" {
					return fail(reason)
				}
			}
		}
	}
	if len(offsets) != len(ids) {
		return fail("incomplete_offsets")
	}
	list := kmsg.NewPtrListOffsetsRequest()
	topic := kmsg.NewListOffsetsRequestTopic()
	topic.Topic = p.topic
	for _, id := range ids {
		partition := kmsg.NewListOffsetsRequestTopicPartition()
		partition.Partition, partition.Timestamp = id, -1
		topic.Partitions = append(topic.Partitions, partition)
	}
	list.Topics = []kmsg.ListOffsetsRequestTopic{topic}
	ends, err := list.RequestWith(ctx, p.requestor)
	if err != nil || ends == nil {
		return fail("log_end_unavailable")
	}
	seen := make(map[int32]bool, len(ids))
	var total int64
	for _, topic := range ends.Topics {
		for _, partition := range topic.Partitions {
			if topic.Topic != p.topic || !wanted[partition.Partition] || seen[partition.Partition] {
				return fail("incomplete_offsets")
			}
			if partition.ErrorCode != 0 || partition.Offset < 0 {
				return fail("log_end_unavailable")
			}
			lag := partition.Offset - offsets[partition.Partition]
			if lag < 0 || total > math.MaxInt64-lag {
				return fail("inconsistent_offsets")
			}
			total += lag
			seen[partition.Partition] = true
		}
	}
	if len(seen) != len(ids) {
		return fail("incomplete_offsets")
	}
	value := float64(total)
	now := time.Now().UTC()
	result.Status, result.Reason, result.Value, result.ObservedAt = "measured", "", &value, &now
	return result
}
