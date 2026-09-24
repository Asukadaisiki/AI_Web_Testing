package main

import (
	"strings"
	"testing"
)

func TestRuntimeConfigUsesBoundedUsageDefaults(t *testing.T) {
	for _, key := range []string{
		"LOOP_MAX_MODEL_CALLS",
		"LOOP_MAX_TOTAL_TOKENS",
		"LOOP_MAX_FRESH_TOTAL_TOKENS",
		"LOOP_MAX_PROMPT_TOKENS_PER_CALL",
		"LOOP_MAX_REQUEST_BYTES",
	} {
		t.Setenv(key, "")
	}

	config := runtimeConfigFromEnv()
	if config.MaxModelCalls != 25 ||
		config.MaxTotalTokens != 1500000 ||
		config.MaxFreshTotalTokens != 300000 ||
		config.MaxPromptTokensPerCall != 30000 ||
		config.MaxRequestBytes != 98304 {
		t.Fatalf("runtime config defaults = %+v", config)
	}
}

func TestRuntimeConfigReadsBoundedUsageEnvironment(t *testing.T) {
	t.Setenv("LOOP_MAX_MODEL_CALLS", "12")
	t.Setenv("LOOP_MAX_TOTAL_TOKENS", "1200")
	t.Setenv("LOOP_MAX_FRESH_TOTAL_TOKENS", "900")
	t.Setenv("LOOP_MAX_PROMPT_TOKENS_PER_CALL", "700")
	t.Setenv("LOOP_MAX_REQUEST_BYTES", "600")

	config := runtimeConfigFromEnv()
	if config.MaxModelCalls != 12 ||
		config.MaxTotalTokens != 1200 ||
		config.MaxFreshTotalTokens != 900 ||
		config.MaxPromptTokensPerCall != 700 ||
		config.MaxRequestBytes != 600 {
		t.Fatalf("runtime config from environment = %+v", config)
	}
}

// 密钥的两种给法必须都成立，且都不能把密钥写进错误信息里。
func TestResolveAPIKeyPrefersTheDirectValue(t *testing.T) {
	t.Setenv("LOOP_LLM_API_KEY", "direct-key")
	t.Setenv("LOOP_LLM_API_KEY_ENV", "LARK_API_KEY")
	t.Setenv("LARK_API_KEY", "indirect-key")

	key, err := resolveAPIKey("lark")
	if err != nil {
		t.Fatalf("resolveAPIKey: %v", err)
	}
	if key != "direct-key" {
		t.Fatalf("key = %q, want the direct value to win", key)
	}
}

// 这条就是火山方舟的配置形态：配置里只写 `apiKeyEnv: LARK_API_KEY`。
func TestResolveAPIKeyReadsTheNamedEnvironmentVariable(t *testing.T) {
	t.Setenv("LOOP_LLM_API_KEY", "")
	t.Setenv("LOOP_LLM_API_KEY_ENV", "LARK_API_KEY")
	t.Setenv("LARK_API_KEY", "ark-secret")

	key, err := resolveAPIKey("lark")
	if err != nil {
		t.Fatalf("resolveAPIKey: %v", err)
	}
	if key != "ark-secret" {
		t.Fatalf("key = %q", key)
	}
}

func TestResolveAPIKeyExplainsAnEmptyNamedVariable(t *testing.T) {
	t.Setenv("LOOP_LLM_API_KEY", "")
	t.Setenv("LOOP_LLM_API_KEY_ENV", "LARK_API_KEY")
	t.Setenv("LARK_API_KEY", "")

	_, err := resolveAPIKey("lark")
	if err == nil {
		t.Fatal("an empty named variable must be an error")
	}
	// 必须点名是哪个变量为空，否则用户只会看到"没密钥"。
	if !strings.Contains(err.Error(), "LARK_API_KEY") {
		t.Fatalf("error must name the variable: %v", err)
	}
}

func TestResolveAPIKeyWithoutAnySourceMentionsBothWays(t *testing.T) {
	t.Setenv("LOOP_LLM_API_KEY", "")
	t.Setenv("LOOP_LLM_API_KEY_ENV", "")

	_, err := resolveAPIKey("lark")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"LOOP_LLM_API_KEY_ENV", "LOOP_LLM_API_KEY", "LOOP_LLM_SCRIPT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must mention %s: %v", want, err)
		}
	}
}

// 提供方默认值：火山方舟只需 LOOP_LLM_PROVIDER=lark + 一个密钥来源就能跑。
func TestProviderDefaults(t *testing.T) {
	if got := defaultBaseURL("lark"); got != "https://ark.cn-beijing.volces.com/api/plan/v3" {
		t.Fatalf("lark base url = %q", got)
	}
	if got := defaultModel("lark"); got != "deepseek-v4.1-flash[1m]" {
		t.Fatalf("lark model = %q", got)
	}
	if got := defaultBaseURL("deepseek"); got != "https://api.deepseek.com" {
		t.Fatalf("deepseek base url = %q", got)
	}
	if got := defaultModel("deepseek"); got != "deepseek-chat" {
		t.Fatalf("deepseek model = %q", got)
	}
}

// 方舟的 base url 不带 /chat/completions，客户端自己追加；这里钉住默认值本身不带，
// 免得以后有人"顺手"把它拼进默认值，变成 .../v3/chat/completions/chat/completions。
func TestLarkBaseURLDoesNotIncludeThePath(t *testing.T) {
	if strings.HasSuffix(defaultBaseURL("lark"), "/chat/completions") {
		t.Fatal("the base url must not include /chat/completions")
	}
}
