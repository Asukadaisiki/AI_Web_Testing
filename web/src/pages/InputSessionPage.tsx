import { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  approveRun,
  answerRun,
  createRun,
  errorMessage,
  getRun,
  getRunCase,
  getRunExecution,
  lastEventOfType,
  payloadString,
  type Run,
} from '../api';
import { CaseStepsTable } from '../components/CaseStepsTable';
import { StatusBadge } from '../components/StatusBadge';
import { UsageLine } from '../components/UsageLine';
import { describeEvent, eventLabel } from '../eventView';
import { useApi } from '../hooks/useApi';
import { useRunEvents } from '../hooks/useRunEvents';

/** 页面 1：输入 / 会话（`/`，run 上下文用 `?run=<run_id>` 指定）。 */
export default function InputSessionPage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const runId = searchParams.get('run');

  const [goal, setGoal] = useState('');
  const [answerText, setAnswerText] = useState('');
  const [busy, setBusy] = useState<'create' | 'approve' | 'answer' | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const run = useApi<Run | null>(
    () => (runId === null ? Promise.resolve(null) : getRun(runId)),
    [runId],
  );
  const events = useRunEvents(runId);

  // 后端每次状态迁移都会发 run_status；收到就重新拉 run，状态一律以后端为准。
  const lastStatusEvent = lastEventOfType(events.events, 'run_status');
  const statusSeq = lastStatusEvent === null ? 0 : lastStatusEvent.seq;
  const reloadRun = run.reload;
  useEffect(() => {
    if (statusSeq > 0) reloadRun();
  }, [statusSeq, reloadRun]);

  // case 就绪事件到达时重新拉 case（404 表示尚未生成，由后端决定）。
  // 但正在 planning 的 run 一定还没有 case：先问一次必然 404，只会在浏览器控制台
  // 留下一条红色错误，把真正的错误盖住。等状态离开 planning 再问。
  const caseEvent = lastEventOfType(events.events, 'case_ready');
  const caseSeq = caseEvent === null ? 0 : caseEvent.seq;
  const caseFetchable = run.data !== null && run.data.status !== 'planning';
  const runCase = useApi(
    () =>
      runId === null || !caseFetchable ? Promise.resolve(null) : getRunCase(runId),
    [runId, caseSeq, caseFetchable],
  );

  // 执行 id 只用于给出跳转链接；仍然来自后端响应。
  const execution = useApi(
    () => (runId === null ? Promise.resolve(null) : getRunExecution(runId)),
    [runId, statusSeq],
  );

  const questionEvent = lastEventOfType(events.events, 'question');
  const question =
    questionEvent === null
      ? null
      : payloadString(questionEvent.payload, 'question') ??
        payloadString(questionEvent.payload, 'text');

  const runStatus = run.data?.status ?? null;

  async function handleCreate(): Promise<void> {
    setBusy('create');
    setActionError(null);
    try {
      const created = await createRun(goal);
      setGoal('');
      navigate(`/?run=${encodeURIComponent(created.run_id)}`);
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function handleApprove(): Promise<void> {
    if (runId === null) return;
    setBusy('approve');
    setActionError(null);
    try {
      await approveRun(runId);
      reloadRun();
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function handleAnswer(): Promise<void> {
    if (runId === null) return;
    setBusy('answer');
    setActionError(null);
    try {
      await answerRun(runId, answerText);
      setAnswerText('');
      reloadRun();
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="stack">
      <section className="card">
        <h2>输入目标</h2>
        <textarea
          className="textarea"
          rows={3}
          value={goal}
          placeholder="描述要达成的目标（前端不做任何执行，只提交给控制面）"
          onChange={(event) => setGoal(event.target.value)}
        />
        <div className="row">
          <button
            type="button"
            className="primary"
            disabled={busy !== null || goal.trim() === ''}
            onClick={() => {
              void handleCreate();
            }}
          >
            {busy === 'create' ? '提交中…' : '开始'}
          </button>
          <span className="muted small">当前 run：{runId ?? '未选择'}</span>
        </div>
        {actionError !== null ? <p className="error-text">{actionError}</p> : null}
      </section>

      {runId === null ? (
        <section className="card">
          <p className="muted">提交目标后开始规划；也可以从左侧 run 列表选一个历史 run。</p>
        </section>
      ) : (
        <>
          <section className="card">
            <h2>run 状态</h2>
            {run.error !== null ? <p className="error-text">{run.error}</p> : null}
            {run.data === null ? (
              <p className="muted">{run.loading ? '加载中…' : '未取到 run'}</p>
            ) : (
              <div className="stack">
                <div className="row">
                  <StatusBadge status={run.data.status} />
                  <span className="mono small">{run.data.id}</span>
                  {run.data.parent_run_id === null || run.data.parent_run_id === '' ? null : (
                    <span className="muted small">来源 run：{run.data.parent_run_id}</span>
                  )}
                </div>
                <div className="muted small">
                  创建 {run.data.created_at} · 更新 {run.data.updated_at}
                </div>
                <UsageLine usage={run.data.usage} />
                <div className="break">{run.data.input}</div>
                <div className="row">
                  <Link to={`/runs/${encodeURIComponent(run.data.id)}/report`}>查看报告</Link>
                  <Link to={`/executions/${encodeURIComponent(run.data.id)}`}>执行详情</Link>
                  <Link to={`/runs/${encodeURIComponent(run.data.id)}/injection`}>错误注入</Link>
                  {execution.data === null || execution.data.execution_id === '' ? null : (
                    <span className="muted small">
                      execution_id：{execution.data.execution_id}
                    </span>
                  )}
                </div>
              </div>
            )}
          </section>

          {runStatus === 'awaiting_approval' ? (
            <section className="card">
              <h2>待审批</h2>
              {runCase.data === null ? (
                <p className="muted">{runCase.loading ? 'case 加载中…' : 'case 尚未生成'}</p>
              ) : (
                <div className="stack">
                  <div className="muted small">case_id：{runCase.data.case_id}</div>
                  <CaseStepsTable artifact={runCase.data.case} />
                  <div className="row">
                    <button
                      type="button"
                      className="primary"
                      disabled={busy !== null}
                      onClick={() => {
                        void handleApprove();
                      }}
                    >
                      {busy === 'approve' ? '提交中…' : '批准并执行'}
                    </button>
                  </div>
                </div>
              )}
              {runCase.error !== null ? <p className="error-text">{runCase.error}</p> : null}
            </section>
          ) : null}

          {runStatus === 'awaiting_input' ? (
            <section className="card">
              <h2>需要回答</h2>
              <p>{question ?? '后端要求补充信息（事件里未带提问文本）'}</p>
              <textarea
                className="textarea"
                rows={3}
                value={answerText}
                placeholder="输入回答"
                onChange={(event) => setAnswerText(event.target.value)}
              />
              <div className="row">
                <button
                  type="button"
                  className="primary"
                  disabled={busy !== null || answerText.trim() === ''}
                  onClick={() => {
                    void handleAnswer();
                  }}
                >
                  {busy === 'answer' ? '提交中…' : '提交回答'}
                </button>
              </div>
            </section>
          ) : null}

          <section className="card">
            <h2>
              事件时间线
              <span className={events.connected ? 'badge badge-pass' : 'badge badge-idle'}>
                {events.connected ? '已连接' : '未连接'}
              </span>
            </h2>
            {events.error !== null ? <p className="muted small">{events.error}</p> : null}
            {events.events.length === 0 ? (
              <p className="muted">暂无事件</p>
            ) : (
              <ul className="timeline">
                {events.events.map((event) => (
                  <li key={String(event.seq)} className={`timeline-item tone-${event.type}`}>
                    <div className="timeline-head">
                      <span className="mono muted">#{event.seq}</span>
                      <span className="badge badge-idle">{eventLabel(event.type)}</span>
                    </div>
                    <div className="break">{describeEvent(event)}</div>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  );
}
