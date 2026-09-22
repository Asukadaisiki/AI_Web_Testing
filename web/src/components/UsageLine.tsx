import { formatTokens, type RunUsage } from '../api';

/**
 * 一个 run 的模型成本。
 *
 * 只展示后端给的事实，不做任何金额换算：单价随模型与时段变，
 * 前端算钱只会算错。要省钱看 reasoning（思考）与 cached（缓存命中）两项。
 */
export function UsageLine({ usage }: { usage: RunUsage }): JSX.Element {
  if (usage.model_calls === 0) {
    return <div className="muted small">模型用量：尚无调用</div>;
  }
  return (
    <div className="muted small">
      模型用量：{usage.model_calls} 次调用 · 输入 {formatTokens(usage.prompt_tokens)} · 输出{' '}
      {formatTokens(usage.completion_tokens)} · 合计 {formatTokens(usage.total_tokens)} tokens
      {usage.reasoning_tokens > 0 ? `（思考 ${formatTokens(usage.reasoning_tokens)}）` : ''}
      {usage.cached_tokens > 0 ? ` · 缓存命中 ${formatTokens(usage.cached_tokens)}` : ''}
    </div>
  );
}
