import { Link, useLocation } from 'react-router-dom';
import { listRuns } from '../api';
import { useApi } from '../hooks/useApi';
import { StatusBadge } from './StatusBadge';

function excerpt(input: string, max = 42): string {
  const flat = input.split(/\s+/).join(' ').trim();
  return flat.length > max ? `${flat.slice(0, max)}…` : flat;
}

/**
 * run 列表（侧边列表）。不是第 5 个页面，只用于在页面间跳转。
 * 数据来自 GET /api/runs?limit=20，不做任何本地状态推断。
 */
export function RunList({ activeRunId }: { activeRunId: string | null }): JSX.Element {
  const location = useLocation();
  const runs = useApi(() => listRuns(20), [location.pathname, location.search]);

  return (
    <div className="run-list">
      <div className="run-list-head">
        <span>run 列表</span>
        <button type="button" className="link-button" onClick={runs.reload}>
          刷新
        </button>
      </div>

      {runs.loading ? <p className="muted">加载中…</p> : null}
      {runs.error !== null ? <p className="error-text">{runs.error}</p> : null}
      {runs.data !== null && runs.data.length === 0 ? (
        <p className="muted">还没有 run</p>
      ) : null}

      <ul>
        {(runs.data ?? []).map((run) => (
          <li key={run.id} className={run.id === activeRunId ? 'active' : ''}>
            <div className="run-item-head">
              <Link to={`/?run=${encodeURIComponent(run.id)}`} title={run.input}>
                {excerpt(run.input) === '' ? run.id : excerpt(run.input)}
              </Link>
            </div>
            <div className="run-item-meta">
              <StatusBadge status={run.status} />
              <span className="mono muted">{run.id}</span>
            </div>
            <div className="run-item-links">
              <Link to={`/runs/${encodeURIComponent(run.id)}/report`}>报告</Link>
              <Link to={`/executions/${encodeURIComponent(run.id)}`}>执行详情</Link>
              <Link to={`/runs/${encodeURIComponent(run.id)}/injection`}>错误注入</Link>
            </div>
            {run.parent_run_id !== null && run.parent_run_id !== '' ? (
              <div className="muted small">来源 run：{run.parent_run_id}</div>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
