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

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/feedback"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/report"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/usage"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/worker"
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
	Store         *store.Store
	Worker        *worker.Client
	LLM           LLM
	MaxModelCalls int
	// MaxTotalTokens 是成本熔断：整个 run 累计超过这个 token 数就中止。
	// 0 表示不限（离线脚本模型本来就不花钱，用它跑不需要熔断）。
	MaxTotalTokens int
	AnswerTimeout  time.Duration
}

// Runtime 驱动闭环：输入 → 规划 → （人审批）→ 执行 → 报告 → 失败回灌候选。
type Runtime struct {
	store         *store.Store
	client        *worker.Client
	llm           LLM
	tools         []planner.Tool
	maxModelCalls int
	maxTokens     int
	answerTimeout time.Duration

	mu       sync.Mutex
	answers  map[string]chan string
	inflight map[string]string
}

// New 创建 Runtime。
func New(config Config) *Runtime {
	maxCalls := config.MaxModelCalls
	if maxCalls <= 0 {
		maxCalls = 40
	}
	answerTimeout := config.AnswerTimeout
	if answerTimeout <= 0 {
		answerTimeout = 30 * time.Minute
	}
	return &Runtime{
		store:         config.Store,
		client:        config.Worker,
		llm:           config.LLM,
		tools:         planner.Tools(),
		maxModelCalls: maxCalls,
		maxTokens:     config.MaxTotalTokens,
		answerTimeout: answerTimeout,
		answers:       map[string]chan string{},
		inflight:      map[string]string{},
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
	total, err := r.store.AddUsage(ctx, runID, spent)
	if err != nil {
		// 记账失败不该拖垮这次 run：把用量写进事件，至少不丢证据。
		r.emit(ctx, runID, EventModelUsage, map[string]any{
			"call": spent, "error": err.Error(),
		})
		return nil
	}
	r.emit(ctx, runID, EventModelUsage, map[string]any{
		"call":  spent,
		"total": total,
		"limit": r.maxTokens,
	})
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
	session, err := planner.New(ctx, r.client, run.Input)
	if err != nil {
		return r.fail(ctx, run.ID, fmt.Sprintf("无法连接执行器（%v）", err))
	}
	defer session.Close(context.WithoutCancel(ctx))

	messages := []Message{
		{Role: RoleSystem, Content: SystemPrompt()},
		{Role: RoleUser, Content: GoalMessage(run.Input)},
	}
	textOnly := 0
	for call := 0; call < r.maxModelCalls; call++ {
		message, spent, err := r.llm.Next(ctx, messages)
		if err != nil {
			return r.fail(ctx, run.ID, fmt.Sprintf("模型调用失败（%v）", err))
		}
		// 先记账再处理输出：这一轮的 token 已经花掉了，哪怕随后预算爆掉也要留下记录。
		if err := r.recordUsage(ctx, run.ID, spent); err != nil {
			return r.fail(ctx, run.ID, err.Error())
		}
		messages = append(messages, message)
		if strings.TrimSpace(message.Content) != "" {
			r.emit(ctx, run.ID, EventAssistant, map[string]any{"text": message.Content})
		}
		if len(message.ToolCalls) == 0 {
			textOnly++
			if textOnly >= 2 {
				return r.fail(ctx, run.ID, "模型连续两轮只输出文本、没有调用任何工具")
			}
			messages = append(messages, Message{Role: RoleUser, Content: NudgeMessage})
			continue
		}
		textOnly = 0

		for _, toolCall := range message.ToolCalls {
			if toolCall.Name == planner.ToolAskUser {
				answer, err := r.askUser(ctx, run, toolCall)
				if err != nil {
					return err
				}
				messages = append(messages, answer)
				continue
			}
			outcome, err := session.Call(ctx, toolCall.Name, toolCall.Arguments)
			if err != nil {
				// 参数不是合法 JSON 之类的内部问题：当成工具结果回给模型，让它改口。
				detail := err.Error()
				r.emit(ctx, run.ID, EventToolCall, map[string]any{
					"tool": toolCall.Name, "ok": false, "error": "tool_arguments_invalid", "detail": detail,
				})
				messages = append(messages, toolMessage(toolCall.ID, map[string]any{
					"ok": false, "error": "tool_arguments_invalid", "detail": detail,
				}))
				continue
			}
			r.emitToolCall(ctx, run.ID, toolCall, outcome)
			encoded, err := json.Marshal(outcome.Result)
			if err != nil {
				return r.fail(ctx, run.ID, fmt.Sprintf("工具结果无法序列化（%v）", err))
			}
			messages = append(messages, Message{
				Role: RoleTool, ToolCallID: toolCall.ID, Content: string(encoded),
			})
			if outcome.Case != nil {
				return r.awaitApproval(ctx, run, *outcome.Case, outcome.DryRun)
			}
		}
	}
	return r.fail(ctx, run.ID, fmt.Sprintf("模型调用次数达到上限 %d，已终止规划", r.maxModelCalls))
}

func toolMessage(callID string, payload any) Message {
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(`{"ok":false,"error":"internal_error"}`)
	}
	return Message{Role: RoleTool, ToolCallID: callID, Content: string(encoded)}
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

func (r *Runtime) askUser(ctx context.Context, run store.Run, call ToolCall) (Message, error) {
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
		return toolMessage(call.ID, map[string]any{"ok": true, "answer": answer}), nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-time.After(r.answerTimeout):
		return Message{}, r.fail(ctx, run.ID, "等待人工回答超时")
	}
}

// awaitApproval 落库 case 并进入 awaiting_approval。
func (r *Runtime) awaitApproval(
	ctx context.Context, run store.Run, artifact contract.Case, dryRun *contract.ExecutionResult,
) error {
	saved, err := r.store.SaveCase(ctx, run.ID, artifact)
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
	result, err := r.client.Execute(ctx, artifact)
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
