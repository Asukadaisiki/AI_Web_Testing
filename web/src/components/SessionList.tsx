import { Link, useLocation } from 'react-router-dom';
import { formatTokens, listSessions } from '../api';
import { useApi } from '../hooks/useApi';
import { StatusBadge } from './StatusBadge';

function excerpt(text: string, max = 40): string {
  const flat = text.split(/\s+/).join(' ').trim();
  return flat.length > max ? `${flat.slice(0, max)}…` : flat;
}

/**
 * 会话列表（侧边列表）。不是第 5 个页面，只用于在会话之间跳转。
 *
 * 列的是**会话**而不是 run：一个会话 = 一个目标 + 它的全部轮次（CONTRACT §9），
 * 所以这里显示的是目标、最新一轮的状态、轮次数与整个会话的累计用量。
 * 数据来自 GET /api/sessions?limit=20，不做任何本地状态推断。
 */
export function SessionList({ activeSessionId }: { activeSessionId: string | null }): JSX.Element {
  const location = useLocation();
  const sessions = useApi(() => listSessions(20), [location.pathname, location.search]);

  return (
    <div className="run-list">
      <div className="run-list-head">
        <span>会话列表</span>
        <button type="button" className="link-button" onClick={sessions.reload}>
          刷新
        </button>
      </div>

      {sessions.loading ? <p className="muted">加载中…</p> : null}
      {sessions.error !== null ? <p className="error-text">{sessions.error}</p> : null}
      {sessions.data !== null && sessions.data.length === 0 ? (
        <p className="muted">还没有会话</p>
      ) : null}

      <ul>
        {(sessions.data ?? []).map((session) => (
          <li key={session.id} className={session.id === activeSessionId ? 'active' : ''}>
            <div className="run-item-head">
              <Link to={`/?session=${encodeURIComponent(session.id)}`} title={session.goal}>
                {excerpt(session.goal) === '' ? session.id : excerpt(session.goal)}
              </Link>
            </div>
            <div className="run-item-meta">
              {session.status === '' ? null : <StatusBadge status={session.status} />}
              <span className="muted small">
                {session.run_count} 轮 · {formatTokens(session.usage.total_tokens)} tokens
              </span>
            </div>
            <div className="run-item-meta">
              <span className="mono muted">{session.id}</span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
