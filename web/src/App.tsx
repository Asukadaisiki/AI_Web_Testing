import { Link, NavLink, Route, Routes, useLocation } from 'react-router-dom';
import { RunList } from './components/RunList';
import ExecutionDetailPage from './pages/ExecutionDetailPage';
import InjectionPage from './pages/InjectionPage';
import InputSessionPage from './pages/InputSessionPage';
import ReportPage from './pages/ReportPage';

/**
 * 从当前地址里取出 run id，仅用于高亮侧边列表。
 * Shell 在 Routes 之外，拿不到 useParams，故直接解析 pathname。
 */
function activeRunIdFrom(pathname: string, search: string): string | null {
  if (pathname === '/') {
    const run = new URLSearchParams(search).get('run');
    return run === '' ? null : run;
  }
  const segments = pathname.split('/').filter((segment) => segment !== '');
  if (segments.length >= 2 && (segments[0] === 'runs' || segments[0] === 'executions')) {
    return decodeURIComponent(segments[1] ?? '') || null;
  }
  return null;
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
  const activeRunId = activeRunIdFrom(location.pathname, location.search);
  return (
    <div className="app">
      <aside className="sidebar">
        <h1>Loop 控制台</h1>
        <nav className="nav">
          <NavLink to="/" end>
            输入 / 会话
          </NavLink>
        </nav>
        <RunList activeRunId={activeRunId} />
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
