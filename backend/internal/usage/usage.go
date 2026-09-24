// Package usage 记录模型调用的 token 用量。
//
// 单独成包是因为它被三边共用：LLM 客户端产出、Runtime 累计并熔断、store 持久化。
// 放在其中任何一边都会逼出反向依赖（agentruntime → store 已经是单向的）。
package usage

// Usage 是一次或累计的模型调用用量。
//
// ModelCalls 也算在内，这样单次用量与累计用量是同一个类型，Add 可以无差别累加。
type Usage struct {
	ModelCalls        int `json:"model_calls"`
	PromptTokens      int `json:"prompt_tokens"`
	CompletionTokens  int `json:"completion_tokens"`
	TotalTokens       int `json:"total_tokens"`
	FreshPromptTokens int `json:"fresh_prompt_tokens"`
	FreshTotalTokens  int `json:"fresh_total_tokens"`
	// ReasoningTokens 是推理模型（如方舟上的 deepseek-v4.1）单独计费的思考 token，
	// 已包含在 CompletionTokens 里，这里只是把它显式暴露出来便于观察成本构成。
	ReasoningTokens int `json:"reasoning_tokens"`
	// CachedTokens 是命中提示词缓存的输入 token，通常是唯一能省下的钱。
	CachedTokens int `json:"cached_tokens"`
}

// Call 构造"一次调用"的用量，并把提供方漏掉的 total 补上。
func Call(prompt, completion, reasoning, cached int) Usage {
	value := Usage{
		ModelCalls:       1,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		ReasoningTokens:  reasoning,
		CachedTokens:     cached,
	}
	return value.Normalize()
}

// Normalize 在提供方没给 total 时用 prompt+completion 补上。
func (u Usage) Normalize() Usage {
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	u.FreshPromptTokens = max(u.PromptTokens-u.CachedTokens, 0)
	u.FreshTotalTokens = u.FreshPromptTokens + u.CompletionTokens
	return u
}

// Add 累加另一次用量。
func (u Usage) Add(other Usage) Usage {
	return Usage{
		ModelCalls:       u.ModelCalls + other.ModelCalls,
		PromptTokens:     u.PromptTokens + other.PromptTokens,
		CompletionTokens: u.CompletionTokens + other.CompletionTokens,
		TotalTokens:      u.TotalTokens + other.TotalTokens,
		ReasoningTokens:  u.ReasoningTokens + other.ReasoningTokens,
		CachedTokens:     u.CachedTokens + other.CachedTokens,
	}.Normalize()
}

// IsZero 表示这一次调用没有报用量（例如离线脚本模型，或提供方不返回 usage）。
func (u Usage) IsZero() bool { return u == Usage{} }
