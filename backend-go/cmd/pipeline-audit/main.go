package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	runID, err := parseRunID(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := run(runID); err != nil {
		log.Fatal(err)
	}
}

func parseRunID(args []string) (string, error) {
	flags := flag.NewFlagSet("pipeline-audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var runID string
	flags.StringVar(&runID, "run-id", "", "AgentRun ID to summarize")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf(
			"unexpected arguments: %s",
			strings.Join(flags.Args(), " "),
		)
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "", errors.New("--run-id is required")
	}
	return runID, nil
}

func run(runID string) error {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	service := agentservice.NewService(agentservice.NewPostgresRepository(database))
	events, err := service.ListEvents(ctx, runID, 0)
	if err != nil {
		return fmt.Errorf("read AgentRun events: %w", err)
	}
	summary, err := agentservice.SummarizePipelineTrace(runID, events)
	if err != nil {
		return fmt.Errorf("summarize pipeline trace: %w", err)
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pipeline trace summary: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(encoded))
	return err
}
