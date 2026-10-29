package runtime

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Only the external SDK is simulated. Kafka group ownership, inbox, capacity,
// assignments, fencing, finalization and release all use their real adapters.
func TestKafkaDrainHandsQueuedDeliveryToSecondWorker(t *testing.T) {
	dsn, brokers := os.Getenv("AGENTTRACE_LEASE_TEST_POSTGRES_DSN"), os.Getenv("AGENTTRACE_LEASE_TEST_KAFKA_BROKERS")
	if dsn == "" || brokers == "" {
		t.Skip("isolated lease PostgreSQL DSN and Kafka brokers are required")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := postgres.NewStore(pool)
	active := kafkaDrainFixture(t, ctx, pool, store, "active")
	queued := kafkaDrainFixture(t, ctx, pool, store, "queued")
	store.SetManagementEnabled(true)
	for _, w := range []postgres.WorkerRegistration{
		{ID: "runtime-a", Role: "runtime", Capacity: 1, HeartbeatTTL: time.Minute},
		{ID: "runtime-b", Role: "runtime", Capacity: 2, HeartbeatTTL: time.Minute},
		{ID: "browser", Role: "browser", Endpoint: "http://assigned:8100", Capacity: 1, HeartbeatTTL: time.Minute},
	} {
		if _, err := store.RegisterWorker(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []event.RunCommand{active, queued} {
		if err := store.BindEnvironmentWorker(ctx, command.EnvironmentID, "browser"); err != nil {
			t.Fatal(err)
		}
	}
	bus := kafkaDrainBus(t, ctx, brokers)
	defer bus.Close()
	group := "agenttrace-test-leases-" + rand.Text()

	releaseActive, activeStarted := make(chan struct{}), make(chan struct{})
	activeDone := make(chan error, 1)
	activeProcess := &drainHarnessProcess{entered: activeStarted, release: releaseActive}
	activeRunner := PiRunner{Process: activeProcess, Store: PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime-b", LeaseTTL: time.Minute}}
	go func() { activeDone <- activeRunner.Run(ctx, active) }()
	defer func() {
		cancel()
		if err := <-activeDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("active SDK shutdown: %v", err)
		}
	}()
	select {
	case <-activeStarted:
	case <-ctx.Done():
		t.Fatal("active attempt did not acquire browser capacity")
	}
	instantRelease := make(chan struct{})
	close(instantRelease)
	makeConsumer := func(worker string, deferred chan struct{}) *Consumer {
		runner := PiRunner{Process: &drainHarnessProcess{release: instantRelease},
			Store: PostgresPiTraceStore{Store: store, RuntimeWorkerID: worker, LeaseTTL: time.Minute}}
		return &Consumer{
			Ready:      func(ctx context.Context) (bool, error) { return store.WorkerReady(ctx, worker) },
			Inbox:      DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
			RetryStore: PostgresRetryStore{Store: store, Consumer: "agenttrace-runtime"}, MaxAttempts: 1,
			Runner: inboxRunnerFunc(func(ctx context.Context, command event.RunCommand) error {
				err := runner.Run(ctx, command)
				if inboxconsumer.IsDeliveryDeferred(err) {
					select {
					case deferred <- struct{}{}:
					default:
					}
				}
				return err
			}),
		}
	}
	message := commandMessage(t, "evt-kafka-drain", queued)
	if _, err := bus.Publish(ctx, runCommandTopic, queued.EnvironmentID, message.Value); err != nil {
		t.Fatal(err)
	}
	deferredA, deferredB := make(chan struct{}, 1), make(chan struct{}, 1)
	consumerA := makeConsumer("runtime-a", deferredA)
	aDone := make(chan error, 1)
	go func() { aDone <- consumerA.Consume(ctx, bus, group) }()
	select {
	case <-deferredA:
	case <-ctx.Done():
		t.Fatal("first worker did not own/defer the queued record")
	}
	if _, err := store.SetWorkerDraining(ctx, "", "runtime-a", "admin", true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-aDone:
		if !errors.Is(err, ErrConsumerPaused) {
			t.Fatalf("drain did not relinquish uncommitted Kafka record: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("drained worker stranded the Kafka partition")
	}
	consumerB := makeConsumer("runtime-b", deferredB)
	bCtx, stopB := context.WithCancel(ctx)
	bDone := make(chan error, 1)
	go func() { bDone <- consumerB.Consume(bCtx, bus, group) }()
	defer func() {
		stopB()
		if err := <-bDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("second consumer shutdown: %v", err)
		}
	}()
	select {
	case <-deferredB:
	case <-ctx.Done():
		t.Fatal("second worker did not acquire the uncommitted delivery")
	}
	var attempts, retries, deadLetters int
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT count(*) FROM harness_attempts WHERE run_id=$1`, queued.RunID), &attempts)
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT count(*) FROM consumer_retry_counts`), &retries)
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic='agenttrace.run.dlq.v1'`), &deadLetters)
	if attempts != 0 || retries != 0 || deadLetters != 0 {
		t.Fatalf("queued contention created attempt/retry/DLQ: %d/%d/%d", attempts, retries, deadLetters)
	}
	close(releaseActive)
	for {
		var complete bool
		piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inbox_events WHERE event_id='evt-kafka-drain' AND processed_at IS NOT NULL)`), &complete)
		if complete {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second worker did not complete after acknowledged browser release")
		case <-time.After(20 * time.Millisecond):
		}
	}
	var workerID, status string
	var used int
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT runtime_worker_id FROM worker_assignments WHERE run_id=$1`, queued.RunID), &workerID)
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, queued.RunID), &status)
	piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if workerID != "runtime-b" || status != "succeeded" || used != 0 {
		t.Fatalf("handoff result: worker=%s status=%s occupied=%d", workerID, status, used)
	}
}

func kafkaDrainFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *postgres.Store, suffix string) event.RunCommand {
	t.Helper()
	project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: suffix})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID, Title: suffix})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID, Content: suffix, AgentID: "test",
		Model: event.ModelRef{Provider: "scripted", Name: "test"}, IdempotencyKey: suffix})
	if err != nil {
		t.Fatal(err)
	}
	env := "env-" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO environments(id,project_id,conversation_id,state) VALUES($1,$2,$3,'ready')`, env, project.ID, conversation.ID); err != nil {
		t.Fatal(err)
	}
	command := commandFor(run.ID)
	command.ProjectID, command.ConversationID, command.EnvironmentID, command.ProfileGeneration = project.ID, conversation.ID, env, 1
	return command
}

type drainHarnessProcess struct {
	entered chan struct{}
	release chan struct{}
}

func (p *drainHarnessProcess) Run(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) error {
	_, err := p.RunWithRelease(ctx, request, onEvent)
	return err
}

func (p *drainHarnessProcess) RunWithRelease(ctx context.Context, r piharness.Request, onEvent func(piharness.Event)) (bool, error) {
	onEvent(harEvent(piharness.EventStarted, r.RunID, r.AttemptID, 1, `{}`))
	if p.entered != nil {
		close(p.entered)
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-p.release:
	}
	onEvent(harEvent(piharness.EventSettled, r.RunID, r.AttemptID, 2, `{"assistant_text":"done","verdict":"succeeded"}`))
	return true, nil
}

// The private topic prevents accidental consumption of another stack's runs.
type isolatedLeaseKafkaBus struct {
	*kafka.Client
	topic string
}

func (b *isolatedLeaseKafkaBus) Publish(ctx context.Context, topic, key string, value []byte) (eventbus.Metadata, error) {
	if topic != runCommandTopic {
		return eventbus.Metadata{}, errors.New("lease test accepts only its private command topic")
	}
	return b.Client.Publish(ctx, b.topic, key, value)
}

func (b *isolatedLeaseKafkaBus) Consume(ctx context.Context, topic, group string, handler eventbus.Handler) error {
	if topic != runCommandTopic {
		return errors.New("lease test accepts only its private command topic")
	}
	return b.Client.Consume(ctx, b.topic, group, func(ctx context.Context, message eventbus.Message) error {
		message.Topic = runCommandTopic
		return handler(ctx, message)
	})
}

func kafkaDrainBus(t *testing.T, ctx context.Context, brokers string) *isolatedLeaseKafkaBus {
	t.Helper()
	seeds := strings.Split(brokers, ",")
	client, err := kafka.New(kafka.Config{Brokers: seeds})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	topic := "agenttrace_test_leases_" + rand.Text()
	req := kmsg.NewPtrCreateTopicsRequest()
	req.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}}
	response, err := req.RequestWith(ctx, admin)
	if err != nil || len(response.Topics) != 1 || response.Topics[0].ErrorCode != 0 {
		client.Close()
		t.Fatalf("create private Kafka topic: %#v %v", response, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req := kmsg.NewPtrDeleteTopicsRequest()
		req.TopicNames = []string{topic}
		req.Topics = []kmsg.DeleteTopicsRequestTopic{{Topic: &topic}}
		if _, err := req.RequestWith(cleanupCtx, admin); err != nil {
			t.Errorf("delete private Kafka topic: %v", err)
		}
	})
	return &isolatedLeaseKafkaBus{Client: client, topic: topic}
}
