package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
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
		cfg.LLMProvider,
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		10*time.Minute,
	)
	if err != nil {
		log.Fatalf("configure LLM: %v", err)
	}
	if cfg.LLMThinkMode {
		model.EnableThinking(cfg.LLMReasoningEffort)
	}
	browserClient, err := browserworker.NewClient(cfg.BrowserWorkerURL, 10*time.Minute)
	if err != nil {
		log.Fatalf("configure Browser Worker: %v", err)
	}
	planningStore := planning.NewPostgresStore(database)
	browserClient.SetContextResolver(browserCapabilityContextResolver(planningStore))
	projectStore := projects.NewPostgresStore(database)
	caseStore := cases.NewPostgresStore(database)
	executionStore := execution.NewStore(database)
	correctionStore := corrections.NewStore(database)
	dslStore := dsl.NewStore(database)
	researchRepository := research.NewPostgresRepository(database)
	researchService := research.NewService(
		researchRepository,
		research.NewPostgresSourceReader(database),
	)
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
		researchService,
	)

	log.Printf("agentservice API listening on %s", cfg.Address)
	server.Spin()
}

func browserCapabilityContextResolver(store planning.Store) browserworker.ContextResolver {
	return func(
		ctx context.Context,
		actorUserID int64,
		projectID int64,
		conversationID string,
	) (map[string]any, error) {
		sessionID, err := strconv.ParseInt(conversationID, 10, 64)
		if err != nil || sessionID < 1 {
			return nil, nil
		}
		detail, err := store.GetSession(ctx, actorUserID, sessionID)
		if err != nil {
			return nil, err
		}
		projectLinked := false
		for _, project := range detail.Session.Projects {
			if project.ID == projectID {
				projectLinked = true
				break
			}
		}
		if !projectLinked {
			return nil, fmt.Errorf("planning session %d is not linked to project %d", sessionID, projectID)
		}
		var requirements struct {
			CleanContext   bool    `json:"clean_context"`
			EntryURLOrPage *string `json:"entry_url_or_page"`
		}
		if err := json.Unmarshal(detail.Session.Requirements, &requirements); err != nil {
			return nil, fmt.Errorf("decode planning session requirements: %w", err)
		}
		browserContext := map[string]any{
			"clean_context": requirements.CleanContext,
		}
		if requirements.EntryURLOrPage != nil {
			browserContext["entry_url_or_page"] = *requirements.EntryURLOrPage
		}
		return browserContext, nil
	}
}
