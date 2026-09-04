package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/llm"
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
	if err := database.PingContext(pingContext); err != nil {
		log.Fatalf("connect database: %v", err)
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
	registry, err := tools.NewRegistry(agentcore.AskUserTool{})
	if err != nil {
		log.Fatalf("configure tools: %v", err)
	}
	engine := agentcore.NewEngine(runService, model, registry, cfg.AgentMaxSteps)
	server := httptransport.NewServer(cfg.Address, engine)

	log.Printf("agentcore API listening on %s", cfg.Address)
	server.Spin()
}
