package main

import (
	"log"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	httptransport "github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/transport/http"
)

func main() {
	cfg := config.Load()
	repository := agentcore.NewMemoryRepository()
	agentService := agentcore.NewService(repository)
	server := httptransport.NewServer(cfg.Address, agentService)

	log.Printf("agentcore API listening on %s", cfg.Address)
	server.Spin()
}
