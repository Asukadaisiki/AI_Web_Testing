import {
  asArray,
  asNumber,
  asString,
  isRecord,
  payloadNumber,
  payloadRecord,
  payloadString,
  type RunEvent,
} from './api';

/** 事件类型的中文标签（纯展示，不改变任何状态语义）。 */
export function eventLabel(type: string): string {
  switch (type) {
    case 'run_status':
      return 'run 状态';
    case 'tool_call':
      return '工具调用';
    case 'observation':
      return '观测摘要';
    case 'assistant':
      return '助手输出';
    case 'model_usage':
      return '模型用量';
    case 'case_ready':
      return 'case 就绪';
    case 'execution_step':
      return '执行步骤';
    case 'execution_done':
      return '执行结束';
    case 'report_ready':
      return '报告就绪';
    case 'error':
      return '错误';
    case 'question':
      return '提问';
    default:
      return type;
  }
}

function jsonPreview(value: unknown, max = 200): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    const text = JSON.stringify(value);
    if (typeof text !== 'string') return '';
    return text.length > max ? `${text.slice(0, max)}…` : text;
  } catch {
    return '';
  }
}

function joinParts(parts: readonly (string | null)[]): string {
  const kept: string[] = [];
  for (const part of parts) {
    if (part !== null && part !== '') kept.push(part);
  }
  return kept.join(' · ');
}

/**
 * 事件时间线的一行摘要。
 * SSE payload 形状在 CONTRACT 里未固定，故只按常见键名做只读提取；
 * 提不到就退化为该 payload 的紧凑 JSON。
 */
export function describeEvent(event: RunEvent): string {
  const payload = event.payload;

  switch (event.type) {
    case 'run_status':
      return payloadString(payload, 'status') ?? '';

    case 'tool_call': {
      const tool =
        payloadString(payload, 'tool') ??
        payloadString(payload, 'name') ??
        payloadString(payload, 'tool_name');
      const args =
        payloadRecord(payload, 'args') ?? payloadRecord(payload, 'arguments') ?? null;
      return joinParts([tool, args === null ? null : jsonPreview(args, 160)]);
    }

    case 'observation': {
      const url = payloadString(payload, 'url');
      const title = payloadString(payload, 'title');
      let count: number | null = null;
      if (isRecord(payload)) {
        const elements = payload['elements'];
        if (Array.isArray(elements)) count = elements.length;
        const observation = payloadRecord(payload, 'observation');
        const actionCandidates =
          observation === null ? null : observation['action_candidates'];
        if (Array.isArray(actionCandidates)) {
          return joinParts([
            url,
            title,
            count === null ? null : `${String(count)} 个元素`,
            `${String(actionCandidates.length)} 个候选动作`,
          ]);
        }
      }
      const countText = count === null ? null : `${String(count)} 个元素`;
      return joinParts([url, title, countText]);
    }

    case 'assistant':
      return joinParts([
        payloadString(payload, 'text') ??
          payloadString(payload, 'content') ??
          payloadString(payload, 'message'),
      ]);

    case 'model_usage': {
      // payload 形如 {call:{...}, total:{...}, limit:N}。
      const call = payloadRecord(payload, 'call');
      const total = payloadRecord(payload, 'total');
      const callText = asNumber(call === null ? null : call['total_tokens']);
      const totalText = asNumber(total === null ? null : total['total_tokens']);
      const limit = payloadNumber(payload, 'limit');
      const parts: (string | null)[] = [
        callText === null ? null : `本次 ${callText.toLocaleString('en-US')}`,
      ];
      if (totalText !== null) {
        parts.push(
          limit !== null && limit > 0
            ? `累计 ${totalText.toLocaleString('en-US')} / ${limit.toLocaleString('en-US')}`
            : `累计 ${totalText.toLocaleString('en-US')}`,
        );
      }
      // 记账失败时后端会带上 error，必须显示出来，否则成本会静默消失。
      parts.push(payloadString(payload, 'error'));
      return joinParts(parts);
    }

    case 'case_ready': {
      const caseId = payloadString(payload, 'case_id');
      const nested = payloadRecord(payload, 'case');
      const name = nested === null ? null : asString(nested['name']);
      const steps = nested === null ? [] : asArray(nested['steps']);
      return joinParts([
        caseId,
        name,
        steps.length > 0 ? `${String(steps.length)} 步` : null,
      ]);
    }

    case 'execution_step': {
      const index = asNumber(isRecord(payload) ? payload['index'] : null);
      return joinParts([
        index === null ? null : `第 ${String(index)} 步`,
        payloadString(payload, 'action'),
        payloadString(payload, 'status'),
      ]);
    }

    case 'execution_done':
      return joinParts([
        payloadString(payload, 'execution_id'),
        payloadString(payload, 'status'),
      ]);

    case 'report_ready':
      return joinParts([
        payloadString(payload, 'run_id'),
        payloadString(payload, 'status'),
        payloadString(payload, 'execution_id'),
      ]);

    case 'error':
      return joinParts([
        payloadString(payload, 'message') ??
          payloadString(payload, 'error') ??
          payloadString(payload, 'detail'),
      ]);

    case 'question':
      return joinParts([
        payloadString(payload, 'question') ?? payloadString(payload, 'text'),
      ]);

    default:
      return jsonPreview(payload, 200);
  }
}
