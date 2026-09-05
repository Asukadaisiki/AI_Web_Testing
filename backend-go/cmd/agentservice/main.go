package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/cases"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/corrections"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/harness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/browserworker"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/llm"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/projects"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
	httptransport "github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/transport/http"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := config.Load()
	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("configure database: %v", err)
	}
	defer database.Close()
	pingContext, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()
	if pingErr := database.PingContext(pingContext); pingErr != nil {
		log.Fatalf("connect database: %v", pingErr)
	}
	var actorExists bool
	if queryErr := database.QueryRowContext(
		pingContext,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`,
		cfg.DefaultActorID,
	).Scan(&actorExists); queryErr != nil || !actorExists {
		log.Fatalf("default actor %d is unavailable: %v", cfg.DefaultActorID, queryErr)
	}

	repository := agentservice.NewPostgresRepository(database)
	runService := agentservice.NewService(repository)
	model, err := llm.NewOpenAIClient(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		10*time.Minute,
	)
	if err != nil {
		log.Fatalf("configure LLM: %v", err)
	}
	browserClient, err := browserworker.NewClient(cfg.BrowserWorkerURL, 10*time.Minute)
	if err != nil {
		log.Fatalf("configure Browser Worker: %v", err)
	}
	planningStore := planning.NewPostgresStore(database)
	projectStore := projects.NewPostgresStore(database)
	caseStore := cases.NewPostgresStore(database)
	executionStore := execution.NewStore(database)
	correctionStore := corrections.NewStore(database)
	dslStore := dsl.NewStore(database)
	controlPlane := tools.NewControlPlaneCapabilities(
		dslStore,
		caseStore,
		executionStore,
		browserClient,
	)
	toolHandlers := []tools.Handler{tools.AskUserTool{}}
	toolHandlers = append(toolHandlers, tools.NewBrowserTools(browserClient)...)
	toolHandlers = append(toolHandlers, tools.NewGenerateDSLTool(controlPlane))
	toolHandlers = append(
		toolHandlers,
		tools.NewExecuteDSLTool(controlPlane),
		tools.NewGetReportTool(controlPlane),
		tools.NewFixAndRetryTool(controlPlane),
	)
	registry, err := tools.NewRegistry(toolHandlers...)
	if err != nil {
		log.Fatalf("configure tools: %v", err)
	}
	engine := harness.New(runService, model, registry, cfg.AgentMaxTurns)
	server := httptransport.NewServer(
		cfg.Address,
		engine,
		cfg.DefaultActorID,
		planningStore,
		projectStore,
		caseStore,
		executionStore,
		correctionStore,
	)

	log.Printf("agentservice API listening on %s", cfg.Address)
	server.Spin()
}
