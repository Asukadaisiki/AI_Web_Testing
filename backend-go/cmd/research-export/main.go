package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type exportOptions struct {
	runID        string
	experimentID string
	output       string
}

func main() {
	options, err := parseOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := run(options); err != nil {
		log.Fatal(err)
	}
}

func parseOptions(args []string) (exportOptions, error) {
	var options exportOptions
	flags := flag.NewFlagSet("research-export", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.runID, "run-id", "", "research run ID to project and export")
	flags.StringVar(
		&options.experimentID,
		"experiment-id",
		"",
		"experiment ID to project and export",
	)
	flags.StringVar(&options.output, "output", "-", "output JSONL path, or - for stdout")
	if err := flags.Parse(args); err != nil {
		return exportOptions{}, err
	}
	options.runID = strings.TrimSpace(options.runID)
	options.experimentID = strings.TrimSpace(options.experimentID)
	options.output = strings.TrimSpace(options.output)
	if (options.runID == "") == (options.experimentID == "") {
		return exportOptions{}, errors.New(
			"exactly one of --run-id or --experiment-id is required",
		)
	}
	if options.output == "" {
		return exportOptions{}, errors.New("--output must not be blank")
	}
	if flags.NArg() != 0 {
		return exportOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return options, nil
}

func run(options exportOptions) error {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("configure database: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	repository := research.NewPostgresRepository(database)
	reader := research.NewPostgresSourceReader(database)
	projector := research.NewProjector()

	runIDs, err := selectedRunIDs(ctx, repository, options.runID, options.experimentID)
	if err != nil {
		return fmt.Errorf("select research runs: %w", err)
	}
	for _, selected := range runIDs {
		if err := refreshProjection(ctx, repository, reader, projector, selected); err != nil {
			return fmt.Errorf("project research run %s: %w", selected, err)
		}
	}
	exporter := research.NewJSONLExporter(repository)
	if err := writeOutput(options.output, func(writer io.Writer) error {
		if options.runID != "" {
			return exporter.ExportRun(ctx, writer, options.runID)
		}
		return exporter.ExportExperiment(ctx, writer, options.experimentID)
	}); err != nil {
		return fmt.Errorf("export trajectory: %w", err)
	}
	return nil
}

func selectedRunIDs(
	ctx context.Context,
	repository *research.PostgresRepository,
	runID, experimentID string,
) ([]string, error) {
	if runID != "" {
		return []string{runID}, nil
	}
	result := make([]string, 0)
	offset := 0
	for {
		runs, err := repository.ListRuns(ctx, research.RunFilter{
			ExperimentID: &experimentID,
			Limit:        research.DefaultExportPageSize,
			Offset:       offset,
		})
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			result = append(result, run.ID)
		}
		if len(runs) < research.DefaultExportPageSize {
			break
		}
		offset += len(runs)
	}
	if len(result) == 0 {
		return nil, research.ErrNotFound
	}
	return result, nil
}

func refreshProjection(
	ctx context.Context,
	repository *research.PostgresRepository,
	reader research.SourceReader,
	projector *research.Projector,
	runID string,
) error {
	expected, err := repository.GetProjectionState(ctx, runID)
	if err != nil {
		return err
	}
	snapshot, err := reader.Read(ctx, runID)
	if err != nil {
		return err
	}
	transitions, manifest, err := projector.Project(snapshot)
	if err != nil {
		return err
	}
	_, _, err = repository.ReplaceProjection(
		ctx, runID, expected, manifest, transitions,
	)
	return err
}

func writeOutput(path string, write func(io.Writer) error) error {
	if path == "-" {
		return write(os.Stdout)
	}
	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".research-export-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := write(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("replace output file: %w", err)
		}
		return err
	}
	return nil
}
