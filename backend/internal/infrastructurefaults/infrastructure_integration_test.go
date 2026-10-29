// Package infrastructurefaults probes real, exclusively owned infrastructure.
package infrastructurefaults

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	v3 "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/api/v3"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	liveredis "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const privateDatabase = "agenttrace_test_infrastructurefault_20261028"

func database(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	if os.Getenv("AGENTTRACE_INFRAFAULT_ENABLED") != "1" {
		t.Skip("opt-in: run integration/local/infrastructure_faults.py")
	}
	config, err := pgxpool.ParseConfig(os.Getenv("AGENTTRACE_INFRAFAULT_POSTGRES_DSN"))
	if err != nil || config.ConnConfig.Database != privateDatabase {
		t.Fatal("refusing database other than exclusively owned infrastructure fault database")
	}
	config.ConnConfig.RuntimeParams["application_name"] = "agenttrace_owned_infrastructure_fault"
	config.MaxConns = 6
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot connect to owned fault database")
	}
	t.Cleanup(pool.Close)
	var name string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil || name != privateDatabase {
		t.Fatal("owned database identity check failed")
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate private database: %v", err)
	}
	return pool, ctx
}

func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) string {
	t.Helper()
	id := label + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := pool.Exec(ctx, "INSERT INTO projects(id,name) VALUES($1,$1)", "prj_"+id)
	require(t, err)
	_, err = pool.Exec(ctx, "INSERT INTO conversations(id,project_id) VALUES($1,$2)", "conv_"+id, "prj_"+id)
	require(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runs(id,project_id,conversation_id,status,specification)
		VALUES($1,$2,$3,'running','{}')`, "run_"+id, "prj_"+id, "conv_"+id)
	require(t, err)
	return "run_" + id
}

func insertEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, run string, seq int) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO run_events(id,run_id,seq,event_type,payload)
		VALUES($1,$2,$3,'infrastructure.probe',$4::jsonb)`,
		fmt.Sprintf("%s_event_%d", run, seq), run, seq, fmt.Sprintf(`{"probe_sequence":%d}`, seq))
	require(t, err)
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require(t, listener.Close())
	for _, shared := range []int{6379, 5432, 9192, 9193, 8180, 8110, 3300} {
		if port == shared {
			t.Fatal("refusing shared listener port")
		}
	}
	return port
}

type child struct {
	cmd     *exec.Cmd
	done    chan error
	stopped bool
}

func startChild(t *testing.T, binary string, args ...string) *child {
	t.Helper()
	path, err := exec.LookPath(binary)
	require(t, err)
	log, err := os.CreateTemp(t.TempDir(), "owned-process-*.log")
	require(t, err)
	t.Cleanup(func() { _ = log.Close() })
	cmd := exec.Command(path, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(), "KAFKA_HEAP_OPTS=-Xms256m -Xmx256m")
	require(t, cmd.Start())
	c := &child{cmd: cmd, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait() }()
	t.Cleanup(func() { c.stop(t) })
	return c
}

func (c *child) signal(t *testing.T, signal syscall.Signal) {
	t.Helper()
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		t.Fatal("refusing signal outside owned exec child")
	}
	require(t, c.cmd.Process.Signal(signal))
}

func (c *child) stop(t *testing.T) {
	t.Helper()
	if c.stopped {
		return
	}
	c.stopped = true
	select {
	case <-c.done:
		return
	default:
	}
	_ = c.cmd.Process.Signal(syscall.SIGCONT)
	_ = c.cmd.Process.Kill()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Error("owned child failed to exit within cleanup bound")
	}
}

func eventually(t *testing.T, bound time.Duration, check func() bool, failure string) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(failure)
}

func evidence(t *testing.T, expected, observed string, metrics map[string]any, topology map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"fault_real": true, "expected": expected, "observed": observed,
		"metrics": metrics, "topology": topology,
	})
	require(t, err)
	t.Log("INFRASTRUCTURE_EVIDENCE " + string(data))
}

type sseFrame struct {
	ID   string
	Type string
	Data string
}

func connectSSE(t *testing.T, ctx context.Context, url, lastID string) (<-chan sseFrame, func()) {
	t.Helper()
	requestCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	require(t, err)
	req.Header.Set("Last-Event-ID", lastID)
	resp, err := http.DefaultClient.Do(req)
	require(t, err)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("real SSE endpoint did not accept stream")
	}
	frames := make(chan sseFrame, 16)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(resp.Body)
		var frame sseFrame
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if frame.ID != "" || frame.Type != "" {
					select {
					case frames <- frame:
					case <-requestCtx.Done():
						return
					}
				}
				frame = sseFrame{}
			} else if strings.HasPrefix(line, "id: ") {
				frame.ID = strings.TrimPrefix(line, "id: ")
			} else if strings.HasPrefix(line, "event: ") {
				frame.Type = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				frame.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	closeStream := func() { cancel(); _ = resp.Body.Close() }
	t.Cleanup(closeStream)
	return frames, closeStream
}

func nextFrame(t *testing.T, frames <-chan sseFrame, id string) sseFrame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok || frame.ID != id || frame.Type != "infrastructure.probe" {
			t.Fatalf("expected durable SSE ID %s, got %+v open=%v", id, frame, ok)
		}
		if frame.Data != fmt.Sprintf(`{"probe_sequence":%s}`, id) &&
			frame.Data != fmt.Sprintf(`{"probe_sequence": %s}`, id) {
			t.Fatal("SSE payload does not match canonical PostgreSQL event")
		}
		return frame
	case <-time.After(2 * time.Second):
		t.Fatalf("durable SSE ID %s not delivered within two seconds", id)
		return sseFrame{}
	}
}

func TestRedisOutageSSEReplayAndBounds(t *testing.T) {
	pool, ctx := database(t)
	run := fixture(t, ctx, pool, "redis")
	port := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	process := startChild(t, "redis-server", "--bind", "127.0.0.1", "--port", strconv.Itoa(port),
		"--save", "", "--appendonly", "no", "--dir", t.TempDir())
	redis := goredis.NewClient(&goredis.Options{
		Addr: address, MaxRetries: -1, DialTimeout: 100 * time.Millisecond,
		ReadTimeout: 200 * time.Millisecond, WriteTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { _ = redis.Close() })
	eventually(t, 5*time.Second, func() bool { return redis.Ping(ctx).Err() == nil }, "owned Redis not ready")
	store := liveredis.NewClient(redis)
	service := stream.NewService(stream.NewPostgreSQLReader(pool), store, stream.Config{
		ReadBlock: 100 * time.Millisecond, PollInterval: 100 * time.Millisecond,
		MaxSubscriptions: 2, SendTimeout: 150 * time.Millisecond,
	})
	server := httptest.NewServer(v3.New(v3.ServerConfig{
		Repository: postgres.NewStore(pool), Stream: service,
		HeartbeatInterval: 100 * time.Millisecond, SSEWriteTimeout: 200 * time.Millisecond,
	}).Handler())
	t.Cleanup(server.Close)
	url := server.URL + "/api/v1/runs/" + run + "/events"
	insertEvent(t, ctx, pool, run, 1)
	frames, closeStream := connectSSE(t, ctx, url, "")
	nextFrame(t, frames, "1")
	require(t, store.Append(ctx, run, live.LiveEvent{Type: "infrastructure.probe", Data: json.RawMessage(`{}`)}))
	eventually(t, time.Second, func() bool { return service.Stats().DurableReads >= 2 }, "healthy Redis never woke durable reader")
	if service.Stats().FallbackReads != 0 {
		t.Fatal("baseline Redis was not healthy")
	}

	started := time.Now()
	process.stop(t)
	if redis.Ping(ctx).Err() == nil {
		t.Fatal("owned Redis outage was not real")
	}
	insertEvent(t, ctx, pool, run, 2)
	nextFrame(t, frames, "2")
	delivery := time.Since(started)
	before := service.Stats()
	time.Sleep(650 * time.Millisecond)
	after := service.Stats()
	reads := after.DurableReads - before.DurableReads
	if after.FallbackReads < 1 || reads < 1 || reads > 10 {
		t.Fatalf("outage polling not bounded: reads=%d fallback=%d", reads, after.FallbackReads)
	}
	closeStream()
	eventually(t, time.Second, func() bool { return service.Stats().Active == 0 }, "SSE cancellation leaked subscription")
	insertEvent(t, ctx, pool, run, 3)
	replayed, closeReplay := connectSSE(t, ctx, url, "1")
	nextFrame(t, replayed, "2")
	nextFrame(t, replayed, "3")
	closeReplay()
	eventually(t, time.Second, func() bool { return service.Stats().Active == 0 }, "replay cancellation leaked subscription")

	// A real PostgreSQL event cannot be delivered to an unread subscription.
	slowCtx, cancelSlow := context.WithCancel(ctx)
	_, slowErrors := service.Subscribe(slowCtx, run, "")
	select {
	case err := <-slowErrors:
		if !errors.Is(err, stream.ErrSlowConsumer) {
			t.Fatalf("unread subscriber expected bounded failure, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unread subscriber exceeded timeout bound")
	}
	cancelSlow()
	eventually(t, time.Second, func() bool { return service.Stats().Active == 0 }, "slow subscriber leaked slot")
	_, closeA := connectSSE(t, ctx, url, "3")
	_, closeB := connectSSE(t, ctx, url, "3")
	eventually(t, time.Second, func() bool { return service.Stats().Active == 2 }, "capacity baseline missing")
	rejectedBefore := service.Stats().Rejected
	rejected, closeRejected := connectSSE(t, ctx, url, "3")
	rejectionForm := "EOF"
	select {
	case frame, open := <-rejected:
		if open && (frame.Type != "error" || !strings.Contains(frame.Data, "stream_unavailable")) {
			t.Fatalf("capacity did not reject third HTTP SSE subscription: %+v", frame)
		}
		if open {
			rejectionForm = "error_frame"
		}
	case <-time.After(time.Second):
		t.Fatal("capacity rejection did not complete")
	}
	if service.Stats().Rejected != rejectedBefore+1 || service.Stats().Active != 2 {
		t.Fatal("third HTTP subscription bypassed the real capacity guard")
	}
	closeRejected()
	closeA()
	closeB()
	eventually(t, time.Second, func() bool { return service.Stats().Active == 0 }, "capacity cleanup leaked slots")
	evidence(t, "Redis loss preserves SSE sequence/replay, bounded polling/capacity/slow delivery",
		"owned Redis killed; HTTP delivered IDs 1,2 then Last-Event-ID=1 replayed 2,3; bounds held",
		map[string]any{"outage_delivery_ms": delivery.Milliseconds(), "poll_window_ms": 650,
			"poll_window_durable_reads": reads, "stats": service.Stats(), "last_event_id": "1",
			"replayed_ids": []int{2, 3}, "subscription_cap": 2, "slow_timeout_ms": 150,
			"capacity_rejection_form": rejectionForm},
		map[string]any{"redis": address, "redis_owned_pid": process.cmd.Process.Pid,
			"http": server.URL, "postgres_database": privateDatabase})
}

func ownedKafka(t *testing.T, ctx context.Context) (*child, *kafka.Client, string) {
	t.Helper()
	broker, controller := freePort(t), freePort(t)
	for controller == broker {
		controller = freePort(t)
	}
	dir := t.TempDir()
	address := fmt.Sprintf("127.0.0.1:%d", broker)
	controlAddress := fmt.Sprintf("127.0.0.1:%d", controller)
	config := filepath.Join(dir, "server.properties")
	properties := []string{
		"process.roles=broker,controller", "node.id=1",
		"controller.quorum.bootstrap.servers=" + controlAddress,
		"listeners=PLAINTEXT://" + address + ",CONTROLLER://" + controlAddress,
		"advertised.listeners=PLAINTEXT://" + address + ",CONTROLLER://" + controlAddress,
		"inter.broker.listener.name=PLAINTEXT", "controller.listener.names=CONTROLLER",
		"listener.security.protocol.map=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
		"log.dirs=" + filepath.Join(dir, "data"), "num.partitions=1",
		"offsets.topic.replication.factor=1", "transaction.state.log.replication.factor=1",
		"transaction.state.log.min.isr=1", "group.initial.rebalance.delay.ms=0",
	}
	require(t, os.WriteFile(config, []byte(strings.Join(properties, "\n")+"\n"), 0600))
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	uuid, err := exec.CommandContext(initCtx, "kafka-storage", "random-uuid").Output()
	require(t, err)
	initialize := exec.CommandContext(initCtx, "kafka-storage", "format", "--standalone",
		"-t", strings.TrimSpace(string(uuid)), "-c", config)
	if err := initialize.Run(); err != nil {
		t.Fatal("owned Kafka storage initialization failed")
	}
	process := startChild(t, "kafka-server-start", config)
	client, err := kafka.New(kafka.Config{Brokers: []string{address}})
	require(t, err)
	t.Cleanup(func() { _ = client.Close() })
	eventually(t, 30*time.Second, func() bool {
		pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		return client.Health(pingCtx) == nil
	}, "owned Kafka never became healthy")
	return process, client, address
}

func envelope(t *testing.T, run, id string) []byte {
	t.Helper()
	payload, err := json.Marshal(event.Envelope[map[string]int]{
		SchemaVersion: "agenttrace.event.v1", EventID: id, EventType: "infrastructure.probe",
		AggregateID: run, OccurredAt: time.Now().UTC(), CorrelationID: run,
		Payload: map[string]int{"probe_sequence": 1},
	})
	require(t, err)
	return payload
}

func TestKafkaPauseRetainsOutboxAndRecovers(t *testing.T) {
	pool, ctx := database(t)
	run := fixture(t, ctx, pool, "kafka")
	process, bus, address := ownedKafka(t, ctx)
	topic := "agenttrace.infrastructure." + strconv.FormatInt(time.Now().UnixNano(), 10)
	admin, err := kgo.NewClient(kgo.SeedBrokers(address))
	require(t, err)
	t.Cleanup(admin.Close)
	create := kmsg.NewPtrCreateTopicsRequest()
	create.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}}
	response, err := create.RequestWith(ctx, admin)
	require(t, err)
	if len(response.Topics) != 1 || response.Topics[0].ErrorCode != 0 {
		t.Fatal("private broker did not create private topic")
	}
	warmCtx, cancelWarm := context.WithTimeout(ctx, 10*time.Second)
	_, err = bus.Publish(warmCtx, topic, "warmup", envelope(t, run, run+"_warmup"))
	cancelWarm()
	require(t, err)
	payload := envelope(t, run, run+"_fault")
	_, err = pool.Exec(ctx, `INSERT INTO outbox_events(id,event_id,topic,key,payload)
		VALUES($1,$1,$2,$3,$4::jsonb)`, run+"_fault", topic, run, payload)
	require(t, err)
	relay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID: "owned-infrastructure-relay", BatchSize: 1,
		LeaseDuration: time.Second, InitialBackoff: 100 * time.Millisecond,
	})
	process.signal(t, syscall.SIGSTOP)
	defer func() { _ = process.cmd.Process.Signal(syscall.SIGCONT) }()
	state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(process.cmd.Process.Pid)).Output()
	require(t, err)
	if !strings.Contains(string(state), "T") {
		t.Fatal("owned Kafka process did not actually stop")
	}
	started := time.Now()
	pauseCtx, cancelPause := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancelPause()
	type relayResult struct {
		published int
		err       error
	}
	relayDone := make(chan relayResult, 1)
	go func() {
		published, err := relay.RelayOnce(pauseCtx)
		relayDone <- relayResult{published, err}
	}()
	// An idempotent in-flight Kafka write need not honor the caller deadline
	// until a broker response. Inspect durable retention before resuming it.
	time.Sleep(750 * time.Millisecond)
	var result relayResult
	returnedBeforeResume := false
	select {
	case result = <-relayDone:
		returnedBeforeResume = true
	default:
	}
	var pending, leaseHeld bool
	var attemptCount int
	require(t, pool.QueryRow(ctx, `SELECT published_at IS NULL, COALESCE(locked_until > now(),false), attempt_count
		FROM outbox_events WHERE id=$1`, run+"_fault").Scan(&pending, &leaseHeld, &attemptCount))
	if !pending {
		t.Fatal("broker outage incorrectly acknowledged/lost durable outbox event")
	}
	process.signal(t, syscall.SIGCONT)
	if !returnedBeforeResume {
		select {
		case result = <-relayDone:
		case <-time.After(10 * time.Second):
			t.Fatal("in-flight relay did not return within ten seconds after owned broker resumed")
		}
	}
	pauseElapsed := time.Since(started)
	if result.err == nil || result.published != 0 {
		t.Fatalf("deadline-expired relay incorrectly acknowledged publication: %+v", result)
	}
	var remaining time.Duration
	var remainingSeconds float64
	require(t, pool.QueryRow(ctx, `SELECT GREATEST(0,EXTRACT(EPOCH FROM
		(COALESCE(locked_until,next_attempt_at)-now())))::float8
		FROM outbox_events WHERE id=$1`, run+"_fault").Scan(&remainingSeconds))
	remaining = time.Duration(remainingSeconds*float64(time.Second)) + 100*time.Millisecond
	time.Sleep(remaining) // Recovery uses the real lease/backoff; no SQL unlock.
	recoveryStarted := time.Now()
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 10*time.Second)
	published, err := relay.RelayOnce(recoveryCtx)
	cancelRecovery()
	require(t, err)
	if published != 1 {
		t.Fatalf("recovered broker did not acknowledge retained event: %d", published)
	}
	var acknowledged bool
	var partition int
	var offset int64
	require(t, pool.QueryRow(ctx, `SELECT published_at IS NOT NULL,broker_partition,broker_offset
		FROM outbox_events WHERE id=$1`, run+"_fault").Scan(&acknowledged, &partition, &offset))
	if !acknowledged || offset < 1 {
		t.Fatal("recovery lacks real broker acknowledgement metadata")
	}
	dedup := consumer.NewDeduplicator(pool, run+"_consumer")
	type delivery struct {
		err    error
		offset int64
	}
	handled := make(chan delivery, 8)
	consumeCtx, cancelConsume := context.WithTimeout(ctx, 15*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- bus.Consume(consumeCtx, topic, run+"_group", func(ctx context.Context, message eventbus.Message) error {
			if message.Key != run {
				return nil
			}
			err := dedup.Handle(ctx, message, func(ctx context.Context, tx consumer.Tx, _ eventbus.Message) error {
				_, err := tx.Exec(ctx, `INSERT INTO run_events(id,run_id,seq,event_type,payload)
					VALUES($1,$2,1,'infrastructure.probe','{"probe_sequence":1}')`, run+"_canonical", run)
				return err
			})
			select {
			case handled <- delivery{err: err, offset: message.Offset}:
			case <-ctx.Done():
			}
			return err
		})
	}()
	t.Cleanup(func() {
		cancelConsume()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned Kafka consumer did not stop")
		}
	})
	select {
	case received := <-handled:
		require(t, received.err)
	case <-time.After(12 * time.Second):
		t.Fatal("actual Kafka consumer did not apply recovered outbox event")
	}
	// Inject a real duplicate through the broker, not a mock consumer call.
	duplicateMetadata, err := bus.Publish(ctx, topic, run, payload)
	require(t, err)
	duplicateDeadline := time.NewTimer(3 * time.Second)
	defer duplicateDeadline.Stop()
	duplicateObserved := false
	for !duplicateObserved {
		select {
		case received := <-handled:
			require(t, received.err)
			duplicateObserved = received.offset == duplicateMetadata.Offset
		case <-duplicateDeadline.C:
			t.Fatal("actual broker duplicate offset was not processed")
		}
	}
	var canonical, inbox int
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM run_events WHERE run_id=$1", run).Scan(&canonical))
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM inbox_events WHERE event_id=$1 AND processed_at IS NOT NULL", run+"_fault").Scan(&inbox))
	if canonical != 1 || inbox != 1 {
		t.Fatalf("broker redelivery duplicated facts: canonical=%d inbox=%d", canonical, inbox)
	}
	evidence(t, "paused broker retains outbox; real lease expiry and broker recovery preserve one canonical fact",
		"owned KRaft Java process SIGSTOP verified; pending retained during pause; deadline-expired relay failed after SIGCONT; lease recovery and broker duplicate deduplicated",
		map[string]any{"pause_publish_ms": pauseElapsed.Milliseconds(), "pending_during_pause": pending,
			"pause_observation_ms": 750, "caller_deadline_ms": 500, "returned_before_resume": returnedBeforeResume,
			"lease_held_during_pause": leaseHeld, "attempt_count_during_pause": attemptCount,
			"lease_wait_ms": remaining.Milliseconds(), "recovery_and_delivery_ms": time.Since(recoveryStarted).Milliseconds(),
			"broker_partition": partition, "broker_offset": offset, "canonical_facts": canonical,
			"processed_inbox": inbox, "explicit_duplicate_offset": duplicateMetadata.Offset,
			"explicit_duplicate_observed": duplicateObserved},
		map[string]any{"kafka": address, "kafka_owned_pid": process.cmd.Process.Pid, "topic": topic,
			"postgres_database": privateDatabase, "state": "unique temporary KRaft directory; replication factor 1"})
}

func TestPostgresSessionFailureRollsBackAndDeduplicates(t *testing.T) {
	pool, ctx := database(t)
	run := fixture(t, ctx, pool, "postgres")
	message := eventbus.Message{Value: envelope(t, run, run+"_fault"), Key: run}
	dedup := consumer.NewDeduplicator(pool, run+"_consumer")
	var killedPID uint32
	var terminationConfirmed bool
	started := time.Now()
	err := dedup.Handle(ctx, message, func(ctx context.Context, tx consumer.Tx, _ eventbus.Message) error {
		_, err := tx.Exec(ctx, `INSERT INTO run_events(id,run_id,seq,event_type,payload)
			VALUES($1,$2,1,'infrastructure.probe','{"probe_sequence":1}')`, run+"_canonical", run)
		if err != nil {
			return err
		}
		killedPID = tx.Conn().PgConn().PID()
		// The independent control connection may terminate only this owned tx.
		err = pool.QueryRow(ctx, `SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE pid=$1 AND pid<>pg_backend_pid() AND datname=current_database()
			  AND datname=$2 AND application_name='agenttrace_owned_infrastructure_fault'`,
			killedPID, privateDatabase).Scan(&terminationConfirmed)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "SELECT 1")
		return err
	})
	if err == nil || !terminationConfirmed {
		t.Fatal("owned backend session termination did not fail closed")
	}
	var factsAfterFault, inboxAfterFault int
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM run_events WHERE run_id=$1", run).Scan(&factsAfterFault))
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM inbox_events WHERE event_id=$1", run+"_fault").Scan(&inboxAfterFault))
	if factsAfterFault != 0 || inboxAfterFault != 0 {
		t.Fatalf("lost PG transaction persisted partial facts/inbox: %d/%d", factsAfterFault, inboxAfterFault)
	}
	calls := 0
	handler := func(ctx context.Context, tx consumer.Tx, _ eventbus.Message) error {
		calls++
		_, err := tx.Exec(ctx, `INSERT INTO run_events(id,run_id,seq,event_type,payload)
			VALUES($1,$2,1,'infrastructure.probe','{"probe_sequence":1}')`, run+"_canonical", run)
		return err
	}
	require(t, dedup.Handle(ctx, message, handler))
	require(t, dedup.Handle(ctx, message, handler))
	var canonical, inbox int
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM run_events WHERE run_id=$1", run).Scan(&canonical))
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM inbox_events WHERE event_id=$1 AND processed_at IS NOT NULL", run+"_fault").Scan(&inbox))
	if canonical != 1 || inbox != 1 || calls != 1 {
		t.Fatalf("PG recovery/redelivery was not exactly once: facts=%d inbox=%d calls=%d", canonical, inbox, calls)
	}
	// A handler failure on a healthy PG session must not be silently acknowledged.
	handlerFailure := errors.New("owned canonical handler rejected event")
	rejectedMessage := eventbus.Message{Value: envelope(t, run, run+"_rejected")}
	err = dedup.Handle(ctx, rejectedMessage, func(context.Context, consumer.Tx, eventbus.Message) error {
		return handlerFailure
	})
	if !errors.Is(err, handlerFailure) {
		t.Fatal("canonical handler failure was incorrectly acknowledged")
	}
	var rejectedInbox int
	require(t, pool.QueryRow(ctx, "SELECT count(*) FROM inbox_events WHERE event_id=$1", run+"_rejected").Scan(&rejectedInbox))
	if rejectedInbox != 0 {
		t.Fatal("rejected handler committed an inbox acknowledgement")
	}
	evidence(t, "loss of one owned PG backend fails closed with zero partial facts; replay and duplicate apply once",
		"owned tx wrote uncommitted event then guarded pg_terminate_backend; inbox+event rolled back; retry+duplicate persisted exactly once",
		map[string]any{"elapsed_ms": time.Since(started).Milliseconds(), "terminated_owned_pid": killedPID,
			"termination_confirmed": terminationConfirmed, "facts_after_fault": factsAfterFault,
			"inbox_after_fault": inboxAfterFault, "canonical_facts_after_recovery": canonical,
			"processed_inbox_after_recovery": inbox, "recovery_handler_calls": calls, "rejected_inbox": rejectedInbox},
		map[string]any{"postgres_database": privateDatabase, "fault_scope": "one own transaction backend only; server not signalled"})
}
