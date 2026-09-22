import { Fragment, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { artifactUrl, getRunExecution, type ExecutionStep } from '../api';
import { SatisfiedBadge, StatusBadge } from '../components/StatusBadge';
import { useApi } from '../hooks/useApi';

function StepDetail({ step }: { step: ExecutionStep }): JSX.Element {
  const screenshot = step.evidence.screenshot_path ?? '';
  return (
    <div className="stack detail">
      <div>
        <h4>条件判定</h4>
        {step.conditions.length === 0 ? (
          <p className="muted">无条件</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>阶段</th>
                <th>类型</th>
                <th>期望值</th>
                <th>结果</th>
                <th>详情</th>
              </tr>
            </thead>
            <tbody>
              {step.conditions.map((condition, index) => (
                <tr key={`${condition.phase}-${condition.type}-${String(index)}`}>
                  <td className="mono">{condition.phase}</td>
                  <td className="mono">{condition.type}</td>
                  <td className="mono break">{condition.value}</td>
                  <td>
                    <SatisfiedBadge satisfied={condition.satisfied} />
                  </td>
                  <td className="small break">{condition.detail ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div>
        <h4>console</h4>
        {step.evidence.console.length === 0 ? (
          <p className="muted">无 console 记录</p>
        ) : (
          <ul className="log-list">
            {step.evidence.console.map((entry, index) => (
              <li key={String(index)}>
                <span className="mono muted">[{entry.level}]</span> <span className="break">{entry.text}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div>
        <h4>network</h4>
        {step.evidence.network.length === 0 ? (
          <p className="muted">无 network 记录</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>方法</th>
                <th>URL</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {step.evidence.network.map((entry, index) => (
                <tr key={String(index)}>
                  <td className="mono">{entry.method}</td>
                  <td className="mono small break">{entry.url}</td>
                  <td className="mono">{entry.status === null ? '—' : entry.status}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div>
        <h4>截图</h4>
        {screenshot === '' ? (
          <p className="muted">无截图</p>
        ) : (
          <div className="stack">
            <div className="mono small break">{screenshot}</div>
            <a href={artifactUrl(screenshot)} target="_blank" rel="noreferrer">
              <img className="shot" src={artifactUrl(screenshot)} alt={`第 ${step.index} 步截图`} />
            </a>
          </div>
        )}
      </div>

      {step.error === null ? null : (
        <div>
          <h4>错误</h4>
          <p className="error-text break">{step.error}</p>
        </div>
      )}
    </div>
  );
}

/**
 * 页面 2：执行详情（`/executions/:id`）。
 * 这里的 `:id` 是 **run id**：证据端点只有 `GET /api/runs/{id}/execution` 一种读法。
 */
export default function ExecutionDetailPage(): JSX.Element {
  const params = useParams<{ id: string }>();
  const runId = params.id ?? '';
  const [expanded, setExpanded] = useState<readonly number[]>([]);

  const execution = useApi(() => getRunExecution(runId), [runId]);

  function toggle(index: number): void {
    setExpanded((previous) =>
      previous.includes(index) ? previous.filter((item) => item !== index) : [...previous, index],
    );
  }

  const result = execution.data?.result ?? null;
  const steps = result === null ? [] : result.steps;

  return (
    <div className="stack">
      <section className="card">
        <h2>执行详情</h2>
        <div className="row">
          <span className="muted small">run</span>
          <span className="mono small">{runId}</span>
          <Link to={`/runs/${encodeURIComponent(runId)}/report`}>报告</Link>
          <Link to={`/runs/${encodeURIComponent(runId)}/injection`}>错误注入</Link>
          <button type="button" className="link-button" onClick={execution.reload}>
            刷新
          </button>
        </div>
        {execution.error !== null ? <p className="error-text">{execution.error}</p> : null}
        {execution.data === null ? (
          <p className="muted">{execution.loading ? '加载中…' : '未取到执行记录'}</p>
        ) : (
          <div className="row">
            <span className="muted small">execution</span>
            <span className="mono small">{execution.data.execution_id}</span>
            <StatusBadge status={execution.data.status} />
            {result === null ? null : (
              <>
                <span className="muted small">final_url</span>
                <span className="mono small break">{result.final_url ?? '—'}</span>
                <span className="muted small">
                  {result.started_at ?? '—'} → {result.finished_at ?? '—'}
                </span>
              </>
            )}
          </div>
        )}
      </section>

      {result === null ? (
        <section className="card">
          <p className="muted">
            {execution.loading ? '执行结果加载中…' : '执行结果尚未生成（后端未返回 result）'}
          </p>
        </section>
      ) : (
        <section className="card">
          <h2>步骤（{steps.length}）</h2>
          <table className="table">
            <thead>
              <tr>
                <th>序号</th>
                <th>动作</th>
                <th>状态</th>
                <th>耗时</th>
                <th>url_before → url_after</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {steps.map((step) => {
                const open = expanded.includes(step.index);
                return (
                  <Fragment key={String(step.index)}>
                    <tr className={open ? 'row-open' : ''}>
                      <td className="mono">{step.index}</td>
                      <td className="mono">{step.action}</td>
                      <td>
                        <StatusBadge status={step.status} />
                      </td>
                      <td className="mono">
                        {step.duration_ms === null ? '—' : `${step.duration_ms} ms`}
                      </td>
                      <td className="mono small break">
                        {step.url_before ?? '—'} → {step.url_after ?? '—'}
                      </td>
                      <td>
                        <button type="button" className="link-button" onClick={() => toggle(step.index)}>
                          {open ? '收起' : '展开'}
                        </button>
                      </td>
                    </tr>
                    {open ? (
                      <tr>
                        <td colSpan={6}>
                          <StepDetail step={step} />
                        </td>
                      </tr>
                    ) : null}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}
