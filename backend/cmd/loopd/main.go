// Command loopd 是 v2 闭环的控制面：输入 → 规划 → 执行 → 报告 → 失败回灌。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentruntime"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/api"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("loopd: %v", err)
	}
}

func run() error {
	root := findProjectRoot()
	dataDir := envOr("LOOP_DATA_DIR", filepath.Join(root, "data"))
	// 证据目录按会话分：<根>/data/sessions/<session_id>/<文件>（CONTRACT §9.2）。
	// 默认值必须与执行器的默认值一致，否则开箱即用时控制面提供的截图会全部 404。
	// 两边都可以用 LOOP_ARTIFACTS_DIR 覆盖。
	artifactsDir := envOr("LOOP_ARTIFACTS_DIR", filepath.Join(root, "data", "sessions"))
	dbPath := envOr("LOOP_DB_PATH", filepath.Join(dataDir, "loop.db"))
	addr := envOr("LOOP_ADDR", "127.0.0.1:8101")
	workerURL := envOr("LOOP_WORKER_URL", "http://127.0.0.1:8100")
	runtimeConfig := runtimeConfigFromEnv()

	if err := api.EnsureArtifactsDir(artifactsDir); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}
	database, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()
	listener, recovered, err := claimControlPlane(context.Background(), addr, database)
	if err != nil {
		return err
	}
	defer listener.Close()
	if recovered > 0 {
		log.Printf("loopd: marked %d interrupted runs as failed", recovered)
	}

	client := worker.New(workerURL)
	model, err := buildLLM()
	if err != nil {
		return err
	}
	runtimeConfig.Store = database
	runtimeConfig.Worker = client
	runtimeConfig.LLM = model
	runtime := agentruntime.New(runtimeConfig)
	server := api.New(database, runtime, client, artifactsDir)
	defer server.Close()

	log.Printf(
		"loopd: db=%s artifacts=%s worker=%s model=%s tools=%d limits=%d calls/%d raw/%d fresh/%d prompt/%d bytes",
		dbPath, artifactsDir, workerURL, model.Label(), len(planner.Tools()),
		runtimeConfig.MaxModelCalls, runtimeConfig.MaxTotalTokens, runtimeConfig.MaxFreshTotalTokens,
		runtimeConfig.MaxPromptTokensPerCall, runtimeConfig.MaxRequestBytes,
	)
	return api.Serve(listener, server.Handler())
}

const interruptedRunReason = "控制面重启中断了正在运行的任务；内存中的规划或执行状态无法恢复，请新开一轮重试"

func claimControlPlane(
	ctx context.Context, addr string, database *store.Store,
) (net.Listener, int, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, 0, fmt.Errorf("listen on %s: %w", addr, err)
	}
	recovered, err := recoverInterruptedRuns(ctx, database)
	if err != nil {
		listener.Close()
		return nil, 0, err
	}
	return listener, recovered, nil
}

func recoverInterruptedRuns(ctx context.Context, database *store.Store) (int, error) {
	recovered, err := database.RecoverInterruptedRuns(ctx, interruptedRunReason)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted runs: %w", err)
	}
	return recovered, nil
}

func runtimeConfigFromEnv() agentruntime.Config {
	return agentruntime.Config{
		MaxModelCalls:          positiveEnvInt("LOOP_MAX_MODEL_CALLS", 25),
		MaxTotalTokens:         nonNegativeEnvInt("LOOP_MAX_TOTAL_TOKENS", 1500000),
		MaxFreshTotalTokens:    nonNegativeEnvInt("LOOP_MAX_FRESH_TOTAL_TOKENS", 300000),
		MaxPromptTokensPerCall: nonNegativeEnvInt("LOOP_MAX_PROMPT_TOKENS_PER_CALL", 30000),
		MaxRequestBytes:        nonNegativeEnvInt("LOOP_MAX_REQUEST_BYTES", 98304),
		AnswerTimeout:          30 * time.Minute,
	}
}

func positiveEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(envOr(key, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func nonNegativeEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(envOr(key, strconv.Itoa(fallback)))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

// buildLLM 依据环境变量选模型。LOOP_LLM_SCRIPT 一旦设置就切到离线脚本回放，
// 这条路径不花钱、可重复，是"先用离线夹具验证闭环"的入口。
func buildLLM() (agentruntime.LLM, error) {
	if scriptPath := os.Getenv("LOOP_LLM_SCRIPT"); scriptPath != "" {
		raw, err := os.ReadFile(scriptPath)
		if err != nil {
			return nil, fmt.Errorf("read LOOP_LLM_SCRIPT %s: %w", scriptPath, err)
		}
		return agentruntime.ScriptedFromJSON(raw)
	}
	provider := envOr("LOOP_LLM_PROVIDER", "deepseek")
	if provider != "deepseek" && provider != "openai" && provider != "lark" {
		return nil, fmt.Errorf(
			"unsupported LOOP_LLM_PROVIDER %q (want deepseek, openai or lark)", provider,
		)
	}
	apiKey, err := resolveAPIKey(provider)
	if err != nil {
		return nil, err
	}
	return agentruntime.NewOpenAILLM(agentruntime.OpenAIConfig{
		BaseURL:     envOr("LOOP_LLM_BASE_URL", defaultBaseURL(provider)),
		APIKey:      apiKey,
		Model:       envOr("LOOP_LLM_MODEL", defaultModel(provider)),
		Temperature: 0,
	}, planner.Tools()), nil
}

// resolveAPIKey 取密钥。两种给法：
//   - LOOP_LLM_API_KEY：直接给值；
//   - LOOP_LLM_API_KEY_ENV：给出**变量名**，从该变量读值（对应配置里的 `apiKeyEnv`）。
//
// 密钥不落盘、不进命令行历史，所以推荐后者。
func resolveAPIKey(provider string) (string, error) {
	if key := os.Getenv("LOOP_LLM_API_KEY"); key != "" {
		return key, nil
	}
	if name := os.Getenv("LOOP_LLM_API_KEY_ENV"); name != "" {
		key := os.Getenv(name)
		if key == "" {
			return "", fmt.Errorf(
				"LOOP_LLM_API_KEY_ENV=%s but that environment variable is empty; export it before starting loopd",
				name,
			)
		}
		return key, nil
	}
	return "", fmt.Errorf(
		"no api key for provider %q: set LOOP_LLM_API_KEY_ENV (e.g. LARK_API_KEY) or LOOP_LLM_API_KEY; "+
			"or set LOOP_LLM_SCRIPT to run the offline scripted model",
		provider,
	)
}

// defaultBaseURL / defaultModel 给出每个提供方的默认值，让 LOOP_LLM_* 只需要
// 设一个 provider（或者再加一个 apiKeyEnv）就能跑。
func defaultBaseURL(provider string) string {
	if provider == "lark" {
		// 火山方舟（Ark）；客户端会自动追加 /chat/completions。
		return "https://ark.cn-beijing.volces.com/api/plan/v3"
	}
	return "https://api.deepseek.com"
}

func defaultModel(provider string) string {
	if provider == "lark" {
		return "deepseek-v4.1-flash[1m]"
	}
	return "deepseek-chat"
}

// findProjectRoot 从工作目录向上找 v2 根（以 CONTRACT.md + backend/ 为标志），
// 这样从 v2/ 或 v2/backend 启动都能定位到同一个 data/。
func findProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if fileExists(filepath.Join(dir, "CONTRACT.md")) && dirExists(filepath.Join(dir, "backend")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
