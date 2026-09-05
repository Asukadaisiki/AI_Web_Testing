package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/llm"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/pythonworker"
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

	repository := agentcore.NewPostgresRepository(database)
	runService := agentcore.NewService(repository)
	model, err := llm.NewOpenAIClient(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		10*time.Minute,
	)
	if err != nil {
		log.Fatalf("configure LLM: %v", err)
	}
	browserClient, err := pythonworker.NewClient(cfg.PythonAPIURL, 10*time.Minute)
	if err != nil {
		log.Fatalf("configure Python browser worker: %v", err)
	}
	toolHandlers := []tools.Handler{tools.AskUserTool{}}
	toolHandlers = append(toolHandlers, tools.NewBrowserTools(browserClient)...)
	toolHandlers = append(toolHandlers, tools.NewGenerateDSLTool(browserClient))
	toolHandlers = append(
		toolHandlers,
		tools.NewExecuteDSLTool(browserClient),
		tools.NewGetReportTool(browserClient),
		tools.NewFixAndRetryTool(browserClient),
	)
	registry, err := tools.NewRegistry(toolHandlers...)
	if err != nil {
		log.Fatalf("configure tools: %v", err)
	}
	engine := agentcore.NewEngine(runService, model, registry, cfg.AgentMaxSteps)
	authenticator, err := authn.NewPythonAuthenticator(cfg.PythonAPIURL, 10*time.Second)
	if err != nil {
		log.Fatalf("configure auth introspection: %v", err)
	}
	planningStore := planning.NewPostgresStore(database)
	server := httptransport.NewServer(
		cfg.Address,
		engine,
		authenticator,
		planningStore,
	)

	log.Printf("agentcore API listening on %s", cfg.Address)
	server.Spin()
}
