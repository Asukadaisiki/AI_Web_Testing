import { Link, useParams } from 'react-router-dom';
import { getRun, getRunReport, type Run, type RunReport } from '../api';
import { SignalKindBadge, StatusBadge } from '../components/StatusBadge';
import { UsageLine } from '../components/UsageLine';
import { useApi } from '../hooks/useApi';

function Summary({ report, run }: { report: RunReport; run: Run | null }): JSX.Element {
  return (
    <>
      <div className="summary">
        <div className="summary-item">
          <span className="muted small">状态</span>
          <StatusBadge status={report.status} />
        </div>
        <div className="summary-item">
          <span className="muted small">步骤数</span>
          <span className="summary-value">{report.steps_total}</span>
        </div>
        <div className="summary-item">
          <span className="muted small">通过</span>
          <span className="summary-value pass-text">{report.steps_passed}</span>
        </div>
        <div className="summary-item">
          <span className="muted small">失败</span>
          <span className="summary-value fail-text">{report.steps_failed}</span>
        </div>
        <div className="summary-item">
          <span className="muted small">耗时</span>
          <span className="summary-value">{report.duration_ms} ms</span>
        </div>
        <div className="summary-item">
          <span className="muted small">execution_id</span>
          <span className="mono small">{report.execution_id ?? '—'}</span>
        </div>
      </div>
      {/* 成本与报告同屏：一次运行的代价和执行结果一样，是报告的一部分。 */}
      {run === null ? null : <UsageLine usage={run.usage} />}
    </>
  );
}

/** 页面 3：报告（`/runs/:id/report`）。 */
export default function ReportPage(): JSX.Element {
  const params = useParams<{ id: string }>();
  const runId = params.id ?? '';
  const report = useApi(() => getRunReport(runId), [runId]);
  // 用量挂在 run 上（执行器不知道模型花了多少），所以这里多拉一次 run。
  const run = useApi(() => getRun(runId), [runId]);

  return (
    <div className="stack">
      <section className="card">
        <h2>报告</h2>
        <div className="row">
          <span className="muted small">run</span>
          <span className="mono small">{runId}</span>
          <button type="button" className="link-button" onClick={report.reload}>
            刷新
          </button>
        </div>
        {report.error !== null ? <p className="error-text">{report.error}</p> : null}
        {report.data === null ? (
          <p className="muted">{report.loading ? '加载中…' : '未取到报告'}</p>
        ) : (
          <Summary report={report.data} run={run.data} />
        )}
        <div className="row">
          <Link to={`/executions/${encodeURIComponent(runId)}`}>跳转到执行详情</Link>
          <Link to={`/runs/${encodeURIComponent(runId)}/injection`}>跳转到错误注入</Link>
          <Link to={`/?run=${encodeURIComponent(runId)}`}>回到输入/会话</Link>
        </div>
      </section>

      <section className="card">
        <h2>失败信号</h2>
        {report.data === null ? (
          <p className="muted">—</p>
        ) : report.data.signals.length === 0 ? (
          <p className="muted">无失败信号</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>步骤</th>
                <th>kind</th>
                <th>message</th>
              </tr>
            </thead>
            <tbody>
              {report.data.signals.map((signal, index) => (
                <tr key={String(index)}>
                  <td className="mono">{signal.step_index === null ? '—' : signal.step_index}</td>
                  <td>
                    <SignalKindBadge kind={signal.kind} />
                  </td>
                  <td className="break">{signal.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
