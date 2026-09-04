package main

import (
	"log"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/platform/llm"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
	httptransport "github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/transport/http"
)

func main() {
	cfg := config.Load()
	repository := agentcore.NewMemoryRepository()
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
