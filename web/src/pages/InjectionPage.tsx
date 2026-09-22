import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  confirmFeedback,
  errorMessage,
  getRun,
  getRunFeedback,
  type FeedbackCandidate,
  type FeedbackCandidateId,
} from '../api';
import { SignalKindBadge, StatusBadge } from '../components/StatusBadge';
import { useApi } from '../hooks/useApi';

function candidateKey(id: FeedbackCandidateId): string {
  return String(id);
}

/** 页面 4：错误注入（`/runs/:id/injection`）。 */
export default function InjectionPage(): JSX.Element {
  const navigate = useNavigate();
  const params = useParams<{ id: string }>();
  const runId = params.id ?? '';

  const run = useApi(() => getRun(runId), [runId]);
  const feedback = useApi(() => getRunFeedback(runId), [runId]);

  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [pending, setPending] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const candidates: FeedbackCandidate[] = feedback.data?.candidates ?? [];

  useEffect(() => {
    setDrafts((previous) => {
      const next: Record<string, string> = {};
      for (const candidate of candidates) {
        const key = candidateKey(candidate.id);
        next[key] = previous[key] ?? candidate.proposed_input;
      }
      return next;
    });
  }, [feedback.data]);

  async function handleConfirm(candidate: FeedbackCandidate): Promise<void> {
    const key = candidateKey(candidate.id);
    const input = drafts[key] ?? candidate.proposed_input;
    setPending(key);
    setActionError(null);
    try {
      const created = await confirmFeedback(runId, candidate.id, input);
      navigate(`/?run=${encodeURIComponent(created.run_id)}`);
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="stack">
      <section className="card">
        <h2>错误注入</h2>
        <div className="row">
          <span className="muted small">run</span>
          <span className="mono small">{runId}</span>
          <button type="button" className="link-button" onClick={feedback.reload}>
            刷新候选
          </button>
          <Link to={`/runs/${encodeURIComponent(runId)}/report`}>报告</Link>
          <Link to={`/executions/${encodeURIComponent(runId)}`}>执行详情</Link>
        </div>
        {run.error !== null ? <p className="error-text">{run.error}</p> : null}
        {run.data === null ? (
          <p className="muted">{run.loading ? 'run 加载中…' : '未取到 run'}</p>
        ) : (
          <div className="stack">
            <div className="row">
              <span className="muted small">本 run 状态</span>
              <StatusBadge status={run.data.status} />
            </div>
            <div className="muted small">
              parent_run_id 来源：
              {run.data.parent_run_id === null || run.data.parent_run_id === ''
                ? '无（本 run 是首轮）'
                : run.data.parent_run_id}
            </div>
            <div className="break">{run.data.input}</div>
          </div>
        )}
      </section>

      <section className="card">
        <h2>失败回灌候选（{candidates.length}）</h2>
        {feedback.error !== null ? <p className="error-text">{feedback.error}</p> : null}
        {feedback.loading && feedback.data === null ? <p className="muted">加载中…</p> : null}
        {!feedback.loading && candidates.length === 0 ? (
          <p className="muted">无候选（后端未为该 run 生成回灌输入）</p>
        ) : null}

        <div className="stack">
          {candidates.map((candidate) => {
            const key = candidateKey(candidate.id);
            return (
              <div className="card inner" key={key}>
                <div className="row">
                  <SignalKindBadge kind={candidate.signal_kind} />
                  <span className="muted small">候选 {key}</span>
                  <span className="muted small">状态：{candidate.status}</span>
                </div>
                <textarea
                  className="textarea"
                  rows={8}
                  value={drafts[key] ?? candidate.proposed_input}
                  onChange={(event) =>
                    setDrafts((previous) => ({ ...previous, [key]: event.target.value }))
                  }
                />
                <div className="row">
                  <button
                    type="button"
                    className="primary"
                    disabled={pending !== null || (drafts[key] ?? candidate.proposed_input).trim() === ''}
                    onClick={() => {
                      void handleConfirm(candidate);
                    }}
                  >
                    {pending === key ? '提交中…' : '确认并开新一轮'}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
        {actionError !== null ? <p className="error-text">{actionError}</p> : null}
      </section>
    </div>
  );
}
