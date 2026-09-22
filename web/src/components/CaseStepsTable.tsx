import type { CaseArtifact, CaseCondition, CaseStep } from '../api';

function ConditionList({ conditions }: { conditions: CaseCondition[] }): JSX.Element {
  if (conditions.length === 0) return <span className="muted">—</span>;
  return (
    <ul className="inline-list">
      {conditions.map((condition, index) => (
        <li key={`${condition.type}-${String(index)}`}>
          <span className="mono">{condition.type}</span>
          <span className="muted"> = </span>
          <span>{condition.value}</span>
          {condition.timeout_ms === null ? null : (
            <span className="muted small"> ({condition.timeout_ms}ms)</span>
          )}
        </li>
      ))}
    </ul>
  );
}

function targetText(step: CaseStep): string {
  if (step.target === null) return '—';
  const { hint, locator, grounding } = step.target;
  const locatorText =
    locator.kind === 'role'
      ? `role=${locator.role ?? ''} name=${locator.name ?? ''}`
      : locator.kind === 'text'
        ? `text=${locator.text ?? ''}`
        : `css=${locator.css ?? ''}`;
  const parts = [hint, `${locator.kind}: ${locatorText}`];
  if (grounding.observation_id !== '') parts.push(`obs=${grounding.observation_id}`);
  return parts.join(' / ');
}

/** case 步骤预览表（CONTRACT §2 的 Step）。只读展示，不做校验推断。 */
export function CaseStepsTable({ artifact }: { artifact: CaseArtifact }): JSX.Element {
  return (
    <div className="stack">
      <div className="muted small">
        {artifact.name} · {artifact.case_version} · 起点 {artifact.base_url} · 共{' '}
        {artifact.steps.length} 步
      </div>
      <table className="table">
        <thead>
          <tr>
            <th>序号</th>
            <th>动作</th>
            <th>意图</th>
            <th>值</th>
            <th>目标</th>
            <th>前置条件</th>
            <th>后置条件</th>
            <th>超时</th>
          </tr>
        </thead>
        <tbody>
          {artifact.steps.map((step) => (
            <tr key={String(step.index)}>
              <td className="mono">{step.index}</td>
              <td className="mono">{step.action}</td>
              <td>{step.intent}</td>
              <td className="mono break">{step.value ?? '—'}</td>
              <td className="small break">{targetText(step)}</td>
              <td>
                <ConditionList conditions={step.preconditions} />
              </td>
              <td>
                <ConditionList conditions={step.postconditions} />
              </td>
              <td className="mono">{step.timeout_ms === null ? '—' : `${step.timeout_ms}ms`}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
