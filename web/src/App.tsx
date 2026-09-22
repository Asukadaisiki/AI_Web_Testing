import { Link, NavLink, Route, Routes, useLocation } from 'react-router-dom';
import { SessionList } from './components/SessionList';
import ExecutionDetailPage from './pages/ExecutionDetailPage';
import InjectionPage from './pages/InjectionPage';
import InputSessionPage from './pages/InputSessionPage';
import ReportPage from './pages/ReportPage';

/**
 * 从当前地址里取出会话 id，仅用于高亮侧边列表。
 * 页面 1 用 `?session=`；报告/执行/注入页用 run id，从 run 反查会话由页面自己负责。
 */
function activeSessionIdFrom(search: string): string | null {
  const session = new URLSearchParams(search).get('session');
  return session === null || session === '' ? null : session;
}

function Fallback(): JSX.Element {
  return (
    <div className="card">
      <h2>路径不存在</h2>
      <p className="muted">本前端只有 4 个页面。</p>
      <Link to="/">回到输入/会话</Link>
    </div>
  );
}

function Shell(): JSX.Element {
  const location = useLocation();
  const activeSessionId = activeSessionIdFrom(location.search);
  return (
    <div className="app">
      <aside className="sidebar">
        <h1>Loop 控制台</h1>
        <nav className="nav">
          <NavLink to="/" end>
            输入 / 会话
          </NavLink>
        </nav>
        <SessionList activeSessionId={activeSessionId} />
      </aside>
      <main className="main">
        <Routes>
          <Route path="/" element={<InputSessionPage />} />
          <Route path="/executions/:id" element={<ExecutionDetailPage />} />
          <Route path="/runs/:id/report" element={<ReportPage />} />
          <Route path="/runs/:id/injection" element={<InjectionPage />} />
          <Route path="*" element={<Fallback />} />
        </Routes>
      </main>
    </div>
  );
}

export default function App(): JSX.Element {
  return <Shell />;
}
