package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/browserworker"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	var concurrency int
	var pollSeconds float64
	flag.IntVar(&concurrency, "concurrency", positiveIntOrDefault("WORKER_CONCURRENCY", 2), "number of job consumers")
	flag.Float64Var(&pollSeconds, "poll-seconds", floatOrDefault("WORKER_POLL_SECONDS", 1), "seconds to wait when no job is available")
	flag.Parse()
	if concurrency < 1 || concurrency > 16 {
		log.Fatalf("concurrency must be between 1 and 16")
	}
	if pollSeconds <= 0 {
		log.Fatalf("poll-seconds must be positive")
	}

	cfg := config.Load()
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("configure database: %v", err)
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect database: %v", err)
	}
	browser, err := browserworker.NewClient(cfg.BrowserWorkerURL, 10*time.Minute)
	if err != nil {
		log.Fatalf("configure Browser Worker: %v", err)
	}
	store := execution.NewStore(db)
	pollInterval := time.Duration(pollSeconds * float64(time.Second))
	hostname, _ := os.Hostname()

	var group sync.WaitGroup
	for index := 0; index < concurrency; index++ {
		group.Add(1)
		workerID := execution.WorkerID(hostname)
		go func() {
			defer group.Done()
			runLoop(ctx, store, browser, workerID, pollInterval)
		}()
	}
	group.Wait()
}

func runLoop(
	ctx context.Context,
	store *execution.Store,
	browser *browserworker.Client,
	workerID string,
	pollInterval time.Duration,
) {
	log.Printf("execution worker started: %s", workerID)
	defer log.Printf("execution worker stopped: %s", workerID)
	for ctx.Err() == nil {
		processed, err := runOnce(ctx, store, browser, workerID)
		if err != nil {
			log.Printf("execution worker %s failed: %v", workerID, err)
		}
		if processed {
			continue
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func runOnce(
	ctx context.Context,
	store *execution.Store,
	browser *browserworker.Client,
	workerID string,
) (bool, error) {
	job, ok, err := store.ClaimNextJob(ctx, workerID, 1800)
	if err != nil || !ok {
		return ok, err
	}
	started, err := store.StartClaimedJobRun(ctx, job)
	if err != nil {
		result := execution.FailedBrowserExecutionResult(err)
		return true, store.FinishClaimedJobRun(ctx, workerID, job, 0, result)
	}
	raw, err := browser.ExecuteBrowserCase(ctx, browserworker.BrowserExecutionRequest{
		ExecutionID: started.ID,
		DSLCase:     started.DSLCase,
		BaseURL:     started.BaseURL,
		InputValues: started.InputValues,
	})
	var result execution.BrowserExecutionResult
	if err != nil {
		result = execution.FailedBrowserExecutionResult(err)
	} else {
		result, err = execution.DecodeBrowserExecutionResult(raw)
		if err != nil {
			result = execution.FailedBrowserExecutionResult(err)
		}
	}
	return true, store.FinishClaimedJobRun(ctx, workerID, job, started.ID, result)
}

func positiveIntOrDefault(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func floatOrDefault(name string, fallback float64) float64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
