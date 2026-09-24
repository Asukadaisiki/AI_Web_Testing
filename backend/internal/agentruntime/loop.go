package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/feedback"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/report"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

// SSE 事件类型，与 web/src/api.ts 的 RunEventType 一一对应。
const (
	EventRunStatus     = "run_status"
	EventToolCall      = "tool_call"
	EventObservation   = "observation"
	EventAssistant     = "assistant"
	EventModelUsage    = "model_usage"
	EventCaseReady     = "case_ready"
	EventExecutionStep = "execution_step"
	EventExecutionDone = "execution_done"
	EventReportReady   = "report_ready"
	EventError         = "error"
	EventQuestion      = "question"
)

// Config 是 Runtime 的构造参数。
type Config struct {
	Store                  *store.Store
	Worker                 *worker.Client
	LLM                    LLM
	MaxModelCalls          int
	MaxFreshTotalTokens    int
	MaxPromptTokensPerCall int
	MaxRequestBytes        int
	// MaxTotalTokens 是成本熔断：整个 run 累计超过这个 token 数就中止。
	// 0 表示不限（离线脚本模型本来就不花钱，用它跑不需要熔断）。
	MaxTotalTokens int
	AnswerTimeout  time.Duration
}

// Runtime 驱动闭环：输入 → 规划 → （人审批）→ 执行 → 报告 → 失败回灌候选。
type Runtime struct {
	store                  *store.Store
	client                 *worker.Client
	llm                    LLM
	tools                  []planner.Tool
	maxModelCalls          int
	maxTokens              int
	maxFreshTokens         int
	maxPromptTokensPerCall int
	maxRequestBytes        int
	answerTimeout          time.Duration

	mu       sync.Mutex
	answers  map[string]chan string
	inflight map[string]string
}

// New 创建 Runtime。
func New(config Config) *Runtime {
	maxCalls := config.MaxModelCalls
	if maxCalls <= 0 {
		maxCalls = 25
	}
	answerTimeout := config.AnswerTimeout
	if answerTimeout <= 0 {
		answerTimeout = 30 * time.Minute
	}
	return &Runtime{
		store:                  config.Store,
		client:                 config.Worker,
		llm:                    config.LLM,
		tools:                  planner.Tools(),
		maxModelCalls:          maxCalls,
		maxTokens:              config.MaxTotalTokens,
		maxFreshTokens:         config.MaxFreshTotalTokens,
		maxPromptTokensPerCall: config.MaxPromptTokensPerCall,
		maxRequestBytes:        config.MaxRequestBytes,
		answerTimeout:          answerTimeout,
		answers:                map[string]chan string{},
		inflight:               map[string]string{},
	}
}

// recordUsage 记下一次模型调用的用量，发事件，并在超出预算时返回错误。
//
// 用 store 作为唯一账本（增量累加并回读总量），Runtime 不另存一份，
// 免得出现"内存里 3 万、库里 2 万"这种没法解释的状态。
func (r *Runtime) recordUsage(ctx context.Context, runID string, spent usage.Usage) error {
	if spent.IsZero() {
		return nil
	}
	spent = spent.Normalize()
	total, accountingErr := r.store.AddUsage(ctx, runID, spent)
	if accountingErr != nil {
		// 事件是尽力而为；即使账本不可用，也要先保留单次调用证据。
		r.emit(ctx, runID, EventModelUsage, map[string]any{
			"call": spent, "error": accountingErr.Error(),
		})
	} else {
		r.emit(ctx, runID, EventModelUsage, map[string]any{
			"call":                         spent,
			"total":                        total,
			"limit":                        r.maxTokens,
			"fresh_total_limit":            r.maxFreshTokens,
			"prompt_tokens_per_call_limit": r.maxPromptTokensPerCall,
		})
	}
	if r.maxPromptTokensPerCall > 0 && spent.PromptTokens > r.maxPromptTokensPerCall {
		return fmt.Errorf(
			"单次模型调用输入 token 超过上限：已用 %d / 上限 %d。"+
				"本 run 已中止；调大 LOOP_MAX_PROMPT_TOKENS_PER_CALL，或缩小请求上下文",
			spent.PromptTokens, r.maxPromptTokensPerCall,
		)
	}
	if accountingErr != nil {
		return fmt.Errorf("模型用量记账失败，本 run 已中止：%w", accountingErr)
	}
	if r.maxFreshTokens > 0 && total.FreshTotalTokens > r.maxFreshTokens {
		return fmt.Errorf(
			"新鲜 token 预算用尽：已用 %d / 上限 %d（%d 次模型调用）。"+
				"本 run 已中止；调大 LOOP_MAX_FRESH_TOTAL_TOKENS，或把目标拆小",
			total.FreshTotalTokens, r.maxFreshTokens, total.ModelCalls,
		)
	}
	if r.maxTokens > 0 && total.TotalTokens > r.maxTokens {
		return fmt.Errorf(
			"token 预算用尽：已用 %d / 上限 %d（%d 次模型调用）。"+
				"本 run 已中止；调大 LOOP_MAX_TOTAL_TOKENS，或把目标拆小",
			total.TotalTokens, r.maxTokens, total.ModelCalls,
		)
	}
	return nil
}

// Tools 暴露给 API，用于展示"模型唯一可用的输出形式"。
func (r *Runtime) Tools() []planner.Tool { return r.tools }

// LLMLabel 说明当前用的是哪个模型。
func (r *Runtime) LLMLabel() string { return r.llm.Label() }

func (r *Runtime) claim(runID, action string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if busy, ok := r.inflight[runID]; ok {
		return fmt.Errorf("run %s is already busy with %s", runID, busy)
	}
	r.inflight[runID] = action
	return nil
}

func (r *Runtime) release(runID string) {
	r.mu.Lock()
	delete(r.inflight, runID)
	r.mu.Unlock()
}

// Answer 把人工回答投递给正在等待的 run。返回 false 表示该 run 没在等人。
func (r *Runtime) Answer(runID, text string) bool {
	r.mu.Lock()
	channel, ok := r.answers[runID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case channel <- text:
		return true
	default:
		return false
	}
}

func (r *Runtime) registerAnswer(runID string) chan string {
	channel := make(chan string, 1)
	r.mu.Lock()
	r.answers[runID] = channel
	r.mu.Unlock()
	return channel
}

func (r *Runtime) unregisterAnswer(runID string) {
	r.mu.Lock()
	delete(r.answers, runID)
	r.mu.Unlock()
}

func (r *Runtime) emit(ctx context.Context, runID, eventType string, payload any) {
	_, _ = r.store.AppendEvent(ctx, runID, eventType, payload)
}

func (r *Runtime) status(ctx context.Context, runID, status string, runErr *string) {
	_ = r.store.UpdateRunStatus(ctx, runID, status, runErr)
	payload := map[string]any{"status": status}
	if runErr != nil {
		payload["error"] = *runErr
	}
	r.emit(ctx, runID, EventRunStatus, payload)
}

// fail 把 run 标记为失败，并返回一个 error 供调用方上抛。
func (r *Runtime) fail(ctx context.Context, runID, message string) error {
	r.emit(ctx, runID, EventError, map[string]any{"message": message})
	_ = r.store.UpdateRunStatus(ctx, runID, store.StatusFailed, &message)
	r.emit(ctx, runID, EventRunStatus, map[string]any{
		"status": store.StatusFailed,
		"error":  message,
	})
	return errors.New(message)
}

// sessionIDOf 取 run 的会话 id（CONTRACT §9）。
//
// 规划与执行都要用它决定产物落哪个目录，所以宁可在这里失败，也不带着空 id 往下跑——
// 否则产物会落到产物根目录下、与所有会话混在一起。
func sessionIDOf(run store.Run) (string, error) {
	if run.SessionID == nil || *run.SessionID == "" {
		return "", fmt.Errorf("run %s 没有关联会话（session_id 为空）", run.ID)
	}
	return *run.SessionID, nil
}

// Plan 跑"输入 → 规划"这一段，直到 case 就绪（进入 awaiting_approval）或失败。
func (r *Runtime) Plan(ctx context.Context, run store.Run) error {
	if err := r.claim(run.ID, "planning"); err != nil {
		return err
	}
	defer r.release(run.ID)

	r.status(ctx, run.ID, store.StatusPlanning, nil)
	// 脚本模型一次只服务一个 run：每个 run 从头回放，离线验证才可重复。
	if restarter, ok := r.llm.(interface{ Restart() }); ok {
		restarter.Restart()
	}
	sessionID, err := sessionIDOf(run)
	if err != nil {
		return r.fail(ctx, run.ID, err.Error())
	}
	session, err := planner.New(ctx, r.client, sessionID, run.Input)
	if err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("无法连接执行器（%v）", err))
	}
	defer session.Close(context.WithoutCancel(ctx))

	textOnly := 0
	totalUsage := usage.Usage{}
	var lastResult *planner.Result
	for call := 0; call < r.maxModelCalls; call++ {
		messages := BuildPlanningMessages(PlanningContext{
			Goal:      run.Input,
			State:     session.Snapshot(lastResult),
			Usage:     totalUsage,
			Remaining: r.remainingBudget(call, totalUsage),
		})
		if err := r.enforceRequestSize(messages); err != nil {
			return r.fail(ctx, run.ID, err.Error())
		}
		message, spent, err := r.llm.Next(ctx, messages)
		if err != nil {
			return r.fail(ctx, run.ID, fmt.Sprintf("模型调用失败（%v）", err))
		}
		// 先记账再处理输出：这一轮的 token 已经花掉了，哪怕随后预算爆掉也要留下记录。
		if err := r.recordUsage(ctx, run.ID, spent); err != nil {
			return r.fail(ctx, run.ID, err.Error())
		}
		totalUsage = totalUsage.Add(spent)
		if strings.TrimSpace(message.Content) != "" {
			r.emit(ctx, run.ID, EventAssistant, map[string]any{"text": message.Content})
		}
		if len(message.ToolCalls) == 0 {
			textOnly++
			if textOnly >= 2 {
				return r.fail(ctx, run.ID, "模型连续两轮只输出文本、没有调用任何工具")
			}
			lastResult = &planner.Result{
				OK:     false,
				Error:  "tool_call_required",
				Detail: NudgeMessage,
			}
			continue
		}
		textOnly = 0

		for _, toolCall := range message.ToolCalls {
			if toolCall.Name == planner.ToolAskUser {
				result, err := r.askUser(ctx, run, toolCall)
				if err != nil {
					return err
				}
				lastResult = &result
				continue
			}
			outcome, err := session.Call(ctx, toolCall.Name, toolCall.Arguments)
			if err != nil {
				// 参数不是合法 JSON 之类的内部问题：当成工具结果回给模型，让它改口。
				detail := err.Error()
				r.emit(ctx, run.ID, EventToolCall, map[string]any{
					"tool": toolCall.Name, "ok": false, "error": "tool_arguments_invalid", "detail": detail,
				})
				lastResult = &planner.Result{
					OK: false, Error: "tool_arguments_invalid", Detail: detail,
				}
				continue
			}
			r.emitToolCall(ctx, run.ID, toolCall, outcome)
			result := outcome.Result
			lastResult = &result
			if outcome.Case != nil {
				return r.awaitApproval(ctx, run, *outcome.Case, outcome.DryRun)
			}
		}
	}
	return r.fail(ctx, run.ID, fmt.Sprintf("模型调用次数达到上限 %d，已终止规划", r.maxModelCalls))
}

func (r *Runtime) enforceRequestSize(messages []Message) error {
	if r.maxRequestBytes <= 0 {
		return nil
	}
	sizer, ok := r.llm.(RequestSizer)
	if !ok {
		return nil
	}
	size, err := sizer.RequestSize(messages)
	if err != nil {
		return fmt.Errorf("模型请求大小计算失败（%v）", err)
	}
	if size > r.maxRequestBytes {
		return fmt.Errorf(
			"模型请求超过大小上限：%d bytes / 上限 %d bytes。"+
				"本 run 已在网络调用前中止；调大 LOOP_MAX_REQUEST_BYTES，或缩小请求上下文",
			size, r.maxRequestBytes,
		)
	}
	return nil
}

func (r *Runtime) remainingBudget(call int, spent usage.Usage) BudgetView {
	return BudgetView{
		ModelCalls:          remaining(r.maxModelCalls, call),
		TotalTokens:         remaining(r.maxTokens, spent.TotalTokens),
		FreshTotalTokens:    remaining(r.maxFreshTokens, spent.FreshTotalTokens),
		PromptTokensPerCall: r.maxPromptTokensPerCall,
		RequestBytesPerCall: r.maxRequestBytes,
	}
}

func remaining(limit, spent int) int {
	if limit <= 0 {
		return 0
	}
	return max(limit-spent, 0)
}

func (r *Runtime) emitToolCall(
	ctx context.Context, runID string, call ToolCall, outcome planner.CallOutcome,
) {
	payload := map[string]any{
		"tool": call.Name,
		"ok":   outcome.Result.OK,
	}
	if json.Valid(call.Arguments) {
		payload["args"] = json.RawMessage(call.Arguments)
	}
	if outcome.Result.Summary != "" {
		payload["summary"] = outcome.Result.Summary
	}
	if outcome.Result.Warning != "" {
		payload["warning"] = outcome.Result.Warning
	}
	if outcome.Result.Error != "" {
		payload["error"] = outcome.Result.Error
	}
	if outcome.Result.Detail != "" {
		payload["detail"] = outcome.Result.Detail
	}
	// 干跑失败必须把"哪一步、哪个条件没过"写进事件。
	// 只写一句 "did not pass a full dry run" 会让运维侧完全瞎掉：
	// 模型在对话里看得到明细，但读事件流的人（和事后复盘）看不到。
	if outcome.Result.Failure != nil {
		payload["failure"] = outcome.Result.Failure
	}
	r.emit(ctx, runID, EventToolCall, payload)

	if page := outcome.Result.Page; page != nil {
		r.emit(ctx, runID, EventObservation, map[string]any{
			"url":         page.URL,
			"title":       page.Title,
			"elements":    len(page.Elements),
			"truncated":   page.Truncated,
			"observation": page,
		})
	}
}

func (r *Runtime) askUser(ctx context.Context, run store.Run, call ToolCall) (planner.Result, error) {
	var args struct {
		Question string `json:"question"`
	}
	_ = json.Unmarshal(call.Arguments, &args)
	question := strings.TrimSpace(args.Question)
	if question == "" {
		question = "请补充完成本次测试所需的信息"
	}
	channel := r.registerAnswer(run.ID)
	defer r.unregisterAnswer(run.ID)

	r.status(ctx, run.ID, store.StatusAwaitingInput, nil)
	r.emit(ctx, run.ID, EventQuestion, map[string]any{"question": question, "text": question})

	select {
	case answer := <-channel:
		r.status(ctx, run.ID, store.StatusPlanning, nil)
		return planner.Result{
			OK: true, Summary: "user supplied requested information", Detail: answer,
		}, nil
	case <-ctx.Done():
		return planner.Result{}, ctx.Err()
	case <-time.After(r.answerTimeout):
		return planner.Result{}, r.fail(ctx, run.ID, "等待人工回答超时")
	}
}

// awaitApproval 落库 case 并进入 awaiting_approval。
func (r *Runtime) awaitApproval(
	ctx context.Context, run store.Run, artifact contract.Case, dryRun *contract.ExecutionResult,
) error {
	sessionID, err := sessionIDOf(run)
	if err != nil {
		return r.fail(ctx, run.ID, err.Error())
	}
	saved, err := r.store.SaveCase(ctx, sessionID, run.ID, artifact)
	if err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("保存 case 失败（%v）", err))
	}
	payload := map[string]any{
		"case_id":      strconv.FormatInt(saved.ID, 10),
		"content_hash": saved.ContentHash,
		"case":         json.RawMessage(saved.Payload),
	}
	if dryRun != nil {
		payload["dry_run_status"] = string(dryRun.Status)
		payload["dry_run_steps"] = len(dryRun.Steps)
	}
	r.emit(ctx, run.ID, EventCaseReady, payload)
	r.status(ctx, run.ID, store.StatusAwaitingApproval, nil)
	return nil
}

// Execute 跑"审批 → 执行 → 报告 → 失败回灌候选"这一段。
func (r *Runtime) Execute(ctx context.Context, run store.Run) error {
	if err := r.claim(run.ID, "executing"); err != nil {
		return err
	}
	defer r.release(run.ID)

	record, err := r.store.GetCase(ctx, run.ID)
	if err != nil {
		return r.fail(ctx, run.ID, "找不到已就绪的 case 工件")
	}
	sessionID, err := sessionIDOf(run)
	if err != nil {
		return r.fail(ctx, run.ID, err.Error())
	}
	// 落库形态必须能直接过契约校验：这是"生成的 DSL 过不了校验"的最后一道防线。
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		return r.executionError(ctx, run, contract.SignalCaseInvalid, err.Error())
	}
	approved, err := r.store.IsCaseApproved(ctx, record.ID)
	if err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("读取审批状态失败（%v）", err))
	}
	if !approved {
		return r.fail(ctx, run.ID, "case 尚未审批，拒绝执行")
	}

	r.status(ctx, run.ID, store.StatusExecuting, nil)
	result, err := r.client.Execute(ctx, sessionID, artifact)
	if err != nil {
		return r.executionError(ctx, run, contract.SignalWorkerError, err.Error())
	}

	if _, err := r.store.SaveExecution(ctx, run.ID, record.ID, result); err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("保存执行结果失败（%v）", err))
	}
	for _, step := range result.Steps {
		payload := map[string]any{
			"index": step.Index, "action": string(step.Action), "status": step.Status,
		}
		if step.Error != nil {
			payload["kind"] = string(step.Error.Kind)
			payload["error"] = step.Error.Message
		}
		r.emit(ctx, run.ID, EventExecutionStep, payload)
	}

	signals := report.Signals(run.ID, result)
	if err := r.store.ReplaceSignals(ctx, run.ID, result.ExecutionID, signals); err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("保存失败信号失败（%v）", err))
	}
	r.status(ctx, run.ID, store.StatusReporting, nil)

	if len(signals) > 0 {
		candidates := feedback.Candidates(run.Input, artifact.Steps, signals)
		if err := r.store.ReplaceFeedback(ctx, run.ID, candidates); err != nil {
			return r.fail(ctx, run.ID, fmt.Sprintf("生成回灌候选失败（%v）", err))
		}
	}

	r.emit(ctx, run.ID, EventExecutionDone, map[string]any{
		"execution_id": result.ExecutionID,
		"status":       string(result.Status),
		"steps":        len(result.Steps),
	})

	// 执行跑完（无论用例通过与否）都算闭环走完；只有执行器自身故障才算 run 失败。
	finalStatus := store.StatusCompleted
	if result.Status == contract.ExecutionError {
		finalStatus = store.StatusFailed
	}
	r.emit(ctx, run.ID, EventReportReady, report.Build(run.ID, finalStatus, result, signals))
	r.status(ctx, run.ID, finalStatus, nil)
	return nil
}

// executionError 处理"执行根本没跑起来"：仍然产出信号与回灌候选，保证报告页有内容。
func (r *Runtime) executionError(
	ctx context.Context, run store.Run, kind contract.SignalKind, message string,
) error {
	signal := store.Signal{
		RunID: run.ID, ExecutionID: "", StepIndex: -1, Kind: string(kind), Message: message,
	}
	signals := []store.Signal{signal}
	_ = r.store.ReplaceSignals(ctx, run.ID, "", signals)
	if err := r.store.ReplaceFeedback(ctx, run.ID, feedback.Candidates(run.Input, nil, signals)); err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("生成回灌候选失败（%v）", err))
	}
	r.emit(ctx, run.ID, EventReportReady, map[string]any{
		"run_id": run.ID, "status": store.StatusFailed, "signals": signals,
	})
	return r.fail(ctx, run.ID, message)
}
