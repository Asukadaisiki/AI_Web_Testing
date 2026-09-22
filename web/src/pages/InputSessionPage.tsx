import { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  approveRun,
  answerRun,
  createSession,
  createRun,
  errorMessage,
  getRun,
  getRunCase,
  getRunExecution,
  getSession,
  lastEventOfType,
  payloadString,
  type Run,
  type SessionDetail,
} from '../api';
import { CaseStepsTable } from '../components/CaseStepsTable';
import { StatusBadge } from '../components/StatusBadge';
import { UsageLine } from '../components/UsageLine';
import { describeEvent, eventLabel } from '../eventView';
import { useApi } from '../hooks/useApi';
import { useRunEvents } from '../hooks/useRunEvents';

/**
 * 页面 1：输入 / 会话。
 *
 * URL 形态：`/?session=<session_id>`，可选 `&run=<run_id>` 指定会话里的某一轮。
 * 会话是目标 + 它的全部轮次（CONTRACT §9），所以这里既显示会话本身（目标、轮次数、
 * 累计用量），也显示它的每一轮，以及当前选中那一轮的状态与时间线。
 */
export default function InputSessionPage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const sessionParam = searchParams.get('session');
  const runParam = searchParams.get('run');

  const [goal, setGoal] = useState('');
  const [answerText, setAnswerText] = useState('');
  const [retryText, setRetryText] = useState('');
  const [busy, setBusy] = useState<'create' | 'approve' | 'answer' | 'retry' | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  // 只给了 run 参数（例如从报告页的链接进来）时，先从 run 反查它的会话。
  const seedRun = useApi<Run | null>(
    () => (runParam === null ? Promise.resolve(null) : getRun(runParam)),
    [runParam],
  );
  const sessionId = sessionParam ?? seedRun.data?.session_id ?? null;

  // 事件流只服务当前选中的那一轮，所以先按 URL 定出 run；没给就用会话的最后一轮。
  const session = useApi<SessionDetail | null>(
    () => (sessionId === null ? Promise.resolve(null) : getSession(sessionId)),
    [sessionId],
  );
  const rounds = session.data?.runs ?? [];
  const latestRoundId = rounds.length === 0 ? null : rounds[rounds.length - 1]!.id;
  const activeRunId = runParam ?? latestRoundId;

  const run = useApi<Run | null>(
    () => (activeRunId === null ? Promise.resolve(null) : getRun(activeRunId)),
    [activeRunId],
  );
  const events = useRunEvents(activeRunId);

  // 后端每次状态迁移都会发 run_status；收到就重新拉 run 与会话，状态一律以后端为准。
  const lastStatusEvent = lastEventOfType(events.events, 'run_status');
  const statusSeq = lastStatusEvent === null ? 0 : lastStatusEvent.seq;
  const reloadRun = run.reload;
  const reloadSession = session.reload;
  useEffect(() => {
    if (statusSeq > 0) {
      reloadRun();
      reloadSession();
    }
  }, [statusSeq, reloadRun, reloadSession]);

  // case 就绪事件到达时重新拉 case（404 表示尚未生成，由后端决定）。
  // 但正在 planning 的 run 一定还没有 case：先问一次必然 404，只会在浏览器控制台
  // 留下一条红色错误，把真正的错误盖住。等状态离开 planning 再问。
  const caseEvent = lastEventOfType(events.events, 'case_ready');
  const caseSeq = caseEvent === null ? 0 : caseEvent.seq;
  const caseFetchable = run.data !== null && run.data.status !== 'planning';
  const runCase = useApi(
    () =>
      activeRunId === null || !caseFetchable ? Promise.resolve(null) : getRunCase(activeRunId),
    [activeRunId, caseSeq, caseFetchable],
  );

  // 执行 id 只用于给出跳转链接；仍然来自后端响应。
  const execution = useApi(
    () => (activeRunId === null ? Promise.resolve(null) : getRunExecution(activeRunId)),
    [activeRunId, statusSeq],
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
      const created = await createSession(goal);
      setGoal('');
      navigate(
        `/?session=${encodeURIComponent(created.session_id)}&run=${encodeURIComponent(created.run_id)}`,
      );
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function handleApprove(): Promise<void> {
    if (activeRunId === null) return;
    setBusy('approve');
    setActionError(null);
    try {
      await approveRun(activeRunId);
      reloadRun();
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function handleAnswer(): Promise<void> {
    if (activeRunId === null) return;
    setBusy('answer');
    setActionError(null);
    try {
      await answerRun(activeRunId, answerText);
      setAnswerText('');
      reloadRun();
    } catch (err: unknown) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function handleRetry(): Promise<void> {
    if (sessionId === null || run.data === null) return;
    setBusy('retry');
    setActionError(null);
    // 输入留空 = 原样重跑当前轮的 input（后端要求 input 非空，所以这里必须回退）。
    const nextInput = retryText.trim() === '' ? run.data.input : retryText;
    try {
      // 新的一轮属于同一个会话（CONTRACT §9.1）；URL 跳到新轮次上。
      const created = await createRun(sessionId, nextInput, activeRunId);
      setRetryText('');
      navigate(
        `/?session=${encodeURIComponent(created.session_id)}&run=${encodeURIComponent(created.run_id)}`,
      );
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
          <span className="muted small">当前会话：{sessionId ?? '未选择'}</span>
        </div>
        {actionError !== null ? <p className="error-text">{actionError}</p> : null}
      </section>

      {sessionId === null ? (
        <section className="card">
          <p className="muted">提交目标后开始规划；也可以从左侧会话列表选一个历史会话。</p>
        </section>
      ) : (
        <>
          <section className="card">
            <h2>会话</h2>
            {session.error !== null ? <p className="error-text">{session.error}</p> : null}
            {session.data === null ? (
              <p className="muted">{session.loading ? '加载中…' : '未取到会话'}</p>
            ) : (
              <div className="stack">
                <div className="row">
                  {session.data.session.status === '' ? null : (
                    <StatusBadge status={session.data.session.status} />
                  )}
                  <span className="mono small">{session.data.session.id}</span>
                  <span className="muted small">{session.data.session.run_count} 轮</span>
                </div>
                <div className="muted small">
                  创建 {session.data.session.created_at} · 更新 {session.data.session.updated_at}
                </div>
                <UsageLine usage={session.data.session.usage} />
                <div className="break">{session.data.session.goal}</div>
              </div>
            )}
          </section>

          <section className="card">
            <h2>轮次</h2>
            {rounds.length === 0 ? (
              <p className="muted">{session.loading ? '加载中…' : '这个会话还没有轮次'}</p>
            ) : (
              <table className="table">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>状态</th>
                    <th>用量</th>
                    <th>run</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rounds.map((round, index) => (
                    <tr
                      key={round.id}
                      className={round.id === activeRunId ? 'row-open' : undefined}
                    >
                      <td>{index + 1}</td>
                      <td>
                        <StatusBadge status={round.status} />
                        {round.parent_run_id === null || round.parent_run_id === '' ? null : (
                          <span className="muted small"> 回灌</span>
                        )}
                      </td>
                      <td className="muted small">
                        {round.usage.model_calls === 0
                          ? '—'
                          : `${round.usage.model_calls} 次 / ${round.usage.total_tokens} tokens`}
                      </td>
                      <td className="mono small">
                        <Link
                          to={`/?session=${encodeURIComponent(sessionId)}&run=${encodeURIComponent(round.id)}`}
                        >
                          {round.id}
                        </Link>
                      </td>
                      <td className="row">
                        <Link to={`/runs/${encodeURIComponent(round.id)}/report`}>报告</Link>
                        <Link to={`/executions/${encodeURIComponent(round.id)}`}>执行</Link>
                        <Link to={`/runs/${encodeURIComponent(round.id)}/injection`}>注入</Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          <section className="card">
            <h2>当前轮次</h2>
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

          {/*
            手动再开一轮：只对已结束的轮次开放（planning/executing 中再开一轮只会
            和当前轮互相踩；回灌的正规入口是错误注入页）。输入留空 = 原样重跑当前轮的 input。
          */}
          {run.data !== null &&
          (run.data.status === 'completed' || run.data.status === 'failed') ? (
            <section className="card">
              <h2>在同一会话里再开一轮</h2>
              <textarea
                className="textarea"
                rows={2}
                value={retryText}
                placeholder={
                  run.data.input === ''
                    ? '输入新一轮的目标（留空则原样重跑）'
                    : `留空则原样重跑：${run.data.input}`
                }
                onChange={(event) => setRetryText(event.target.value)}
              />
              <div className="row">
                <button
                  type="button"
                  className="primary"
                  disabled={busy !== null}
                  onClick={() => {
                    void handleRetry();
                  }}
                >
                  {busy === 'retry' ? '提交中…' : '开新一轮'}
                </button>
                <span className="muted small">
                  新轮次属于同一个会话（CONTRACT §9.1），可在上方轮次表里看到整条链条。
                </span>
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
