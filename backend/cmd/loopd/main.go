// Command loopd 是 v2 闭环的控制面：输入 → 规划 → 执行 → 报告 → 失败回灌。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/agentruntime"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/api"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("loopd: %v", err)
	}
}

func run() error {
	root := findProjectRoot()
	dataDir := envOr("LOOP_DATA_DIR", filepath.Join(root, "data"))
	// 证据目录默认与执行器的默认值一致（v2/data/artifacts），否则开箱即用时
	// 控制面提供的截图会全部 404。两边都可以用 LOOP_ARTIFACTS_DIR 覆盖。
	artifactsDir := envOr("LOOP_ARTIFACTS_DIR", filepath.Join(root, "data", "artifacts"))
	dbPath := envOr("LOOP_DB_PATH", filepath.Join(dataDir, "loop.db"))
	addr := envOr("LOOP_ADDR", "127.0.0.1:8101")
	workerURL := envOr("LOOP_WORKER_URL", "http://127.0.0.1:8100")
	maxModelCalls, err := strconv.Atoi(envOr("LOOP_MAX_MODEL_CALLS", "40"))
	if err != nil || maxModelCalls <= 0 {
		maxModelCalls = 40
	}
	// 成本熔断：默认 150 万 token（一次正常规划在几万量级，留足重试空间）。
	// 设 0 关闭；离线脚本模型不报用量，本来就不会触发。
	maxTotalTokens, err := strconv.Atoi(envOr("LOOP_MAX_TOTAL_TOKENS", "1500000"))
	if err != nil || maxTotalTokens < 0 {
		maxTotalTokens = 1500000
	}

	if err := api.EnsureArtifactsDir(artifactsDir); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}
	database, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	client := worker.New(workerURL)
	model, err := buildLLM()
	if err != nil {
		return err
	}
	runtime := agentruntime.New(agentruntime.Config{
		Store:          database,
		Worker:         client,
		LLM:            model,
		MaxModelCalls:  maxModelCalls,
		MaxTotalTokens: maxTotalTokens,
		AnswerTimeout:  30 * time.Minute,
	})
	server := api.New(database, runtime, client, artifactsDir)
	defer server.Close()

	log.Printf("loopd: db=%s artifacts=%s worker=%s model=%s tools=%d budget=%d calls/%d tokens",
		dbPath, artifactsDir, workerURL, model.Label(), len(planner.Tools()), maxModelCalls, maxTotalTokens)
	return api.Serve(addr, server.Handler())
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
