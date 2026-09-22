export type BadgeTone = 'pass' | 'fail' | 'running' | 'idle';

/** 状态色：通过=绿、失败=红、进行中=蓝/灰、未知=灰。 */
export function toneOf(status: string): BadgeTone {
  switch (status) {
    case 'passed':
    case 'completed':
      return 'pass';
    case 'failed':
    case 'error':
      return 'fail';
    case 'planning':
    case 'awaiting_approval':
    case 'awaiting_input':
    case 'executing':
    case 'reporting':
      return 'running';
    default:
      return 'idle';
  }
}

export function StatusBadge({ status }: { status: string }): JSX.Element {
  const text = status === '' ? '未知' : status;
  return <span className={`badge badge-${toneOf(status)}`}>{text}</span>;
}

/** 布尔判定结果徽标（条件是否满足）。 */
export function SatisfiedBadge({ satisfied }: { satisfied: boolean }): JSX.Element {
  return (
    <span className={`badge badge-${satisfied ? 'pass' : 'fail'}`}>
      {satisfied ? '满足' : '未满足'}
    </span>
  );
}

export function SignalKindBadge({ kind }: { kind: string }): JSX.Element {
  return <span className="badge badge-fail">{kind}</span>;
}
