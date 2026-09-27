import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUpRight, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { APIError, api } from './api'
import { useI18n } from './i18n'
import type { RouteState } from './route'
import type { DaemonDispatch, DaemonEvent, DaemonInstance, Identity } from './types'

const copy = {
  en: {
    title: 'Local agents', intro: 'See which Daemons are in touch with Kairos and what they last reported.',
    present: 'Running or awaiting contact', past: 'Stopped', noPresent: 'No Daemons have reported recently.',
    older: 'Show older instances', recentOnly: 'Show recent instances', choose: 'Open an instance', back: 'All Daemons',
    refresh: 'Refresh', loading: 'Reading Daemons…', unavailable: 'Kairos could not refresh these observations. The last received report may still be shown.',
    missing: 'This Daemon could not be found.', reporting: 'Recently reported', stale: 'No recent report', stopped: 'Stopped', daemonStopping: 'Stopping',
    reportingActive: '{count} dispatches in progress', reportingOne: 'One dispatch in progress', reportingIdle: 'No dispatch in progress',
    reportingPaused: 'Paused before taking new work', stoppingSummary: 'Shutting down; no new work will be accepted.',
    staleSummary: 'Kairos has not heard from this Daemon recently. Its last report arrived {time}; current work is unknown.',
    stoppedSummary: 'This Daemon reported that it stopped. Its last report arrived {time}.',
    lastSeen: 'Last report {time}', lastSnapshot: 'The last report showed {count} dispatches in progress.', lastSnapshotOne: 'One dispatch was in progress in the last report.',
    staleWorkUnknown: 'Current work cannot be confirmed.', stoppedNoWork: 'No unfinished dispatch was recorded at exit.',
    current: 'Current work', lastWork: 'Work in the last report', noCurrent: 'No dispatch is in progress.',
    noLastWork: 'No dispatch was recorded in the last report.', omitted: '{count} more dispatches were omitted from the report.',
    activity: 'What happened', noActivity: 'No activity is available from the last 30 days.', loadingActivity: 'Reading recent activity…',
    retention: 'Activity records are retained for 30 days.',
    gap: '{count} events were dropped locally; this history may have gaps.', details: 'Technical details',
    agent: 'Agent identity', instance: 'Instance ID', adapter: 'Adapter', version: 'Version', slots: 'Concurrent slots',
    started: 'Process started', observed: 'Observed at', health: 'Health at last report',
    healthy: 'Ready to accept work', paused: 'Paused admission', unknown: 'Not yet known', dropped: 'Dropped events',
    taskWork: 'Task work', planning: 'Blackboard planning', completion: 'Blackboard completion', acceptance: 'Work acceptance',
    prepared: 'Preparing', claimed: 'Claimed', starting: 'Starting', running: 'Running', finalizing: 'Finishing',
    stopping: 'Stopping', finishedState: 'Finished', lostState: 'Lost',
    notAttempted: 'Claim not attempted', uncertain: 'Claim response uncertain', active: 'Claim active', ended: 'Claim ended',
    attempt: 'Attempt {count}', openWork: 'Open related work', loadMore: 'Load more', outcomeApplied: 'Outcome saved', outcomeUnconfirmed: 'Outcome not confirmed', duration: '{value} ms',
  },
  'zh-CN': {
    title: '本地 Agent', intro: '看看哪些 Daemon 最近联系了 Kairos，以及它们上次报告的工作。',
    present: '运行中或待确认', past: '已停止', noPresent: '最近还没有 Daemon 上报。',
    older: '查看更早的实例', recentOnly: '只看最近实例', choose: '查看运行情况', back: '返回全部 Daemon',
    refresh: '刷新', loading: '正在读取 Daemon…', unavailable: '暂时无法更新观测信息。页面可能仍显示上次收到的数据。',
    missing: '找不到这个 Daemon。', reporting: '最近有上报', stale: '上报中断', stopped: '已停止', daemonStopping: '正在退出',
    reportingActive: '{count} 项调度进行中', reportingOne: '1 项调度进行中', reportingIdle: '当前没有调度',
    reportingPaused: '已暂停领取新工作', stoppingSummary: '正在退出，不再领取新的工作。',
    staleSummary: 'Kairos 最近没有收到这个 Daemon 的报告。最后一次上报于 {time}；当前执行情况无法确认。',
    stoppedSummary: '这个 Daemon 已报告退出。最后一次上报于 {time}。',
    lastSeen: '最后上报 {time}', lastSnapshot: '上次报告显示 {count} 项调度进行中。', lastSnapshotOne: '上次报告显示 1 项调度进行中。',
    staleWorkUnknown: '当前工作无法确认。', stoppedNoWork: '退出时未记录未结束的调度。',
    current: '正在做的工作', lastWork: '上次报告的工作', noCurrent: '当前没有正在处理的调度。',
    noLastWork: '上次报告没有记录进行中的调度。', omitted: '另有 {count} 项调度未列出详情。',
    activity: '做过的事', noActivity: '最近 30 天没有可用的活动记录。', loadingActivity: '正在读取最近活动…',
    retention: '活动记录保留 30 天。',
    gap: '本地丢弃了 {count} 条事件，这段历史可能不完整。', details: '技术信息',
    agent: 'Agent 身份', instance: '实例 ID', adapter: '适配器', version: '版本', slots: '并发槽位',
    started: '进程启动', observed: '服务端观测时间', health: '上次报告的健康状态',
    healthy: '可以接单', paused: '暂停接单', unknown: '尚未确认', dropped: '丢弃的事件',
    taskWork: '执行 Task', planning: '规划 Blackboard', completion: '完成 Blackboard', acceptance: '验收工作',
    prepared: '准备中', claimed: '已领取', starting: '启动中', running: '执行中', finalizing: '收尾中',
    stopping: '停止中', finishedState: '已结束', lostState: '已丢失',
    notAttempted: '尚未领取 Claim', uncertain: 'Claim 结果待确认', active: 'Claim 有效', ended: 'Claim 已结束',
    attempt: '第 {count} 次尝试', openWork: '打开相关工作', loadMore: '加载更多', outcomeApplied: '结果已写入', outcomeUnconfirmed: '结果尚未确认写入', duration: '{value} 毫秒',
  },
} as const

const eventNames: Record<string, [string, string]> = {
  admission_paused: ['Paused taking work', '暂停接单'], admission_resumed: ['Resumed taking work', '恢复接单'],
  claim_acquired: ['Claimed work', '领取了工作'], harness_started: ['Started execution', '开始执行'],
  harness_retry: ['Retried execution', '重新尝试执行'], outcome_applied: ['Saved the outcome', '写入了结果'],
  dispatch_ended: ['Finished a dispatch', '结束了一次调度'], candidate_quarantined: ['Paused this candidate', '暂缓这个候选工作'],
  shutdown_started: ['Began shutting down', '开始退出'], daemon_stopped: ['Stopped', '已退出'],
}

const outcomeNames: Record<string, [string, string]> = {
  completed: ['Completed', '已完成'], decomposed: ['Split into tasks', '已拆分任务'],
  retryable_failure: ['Will retry', '需要重试'], work_item_failure: ['Work failed', '工作失败'],
  human_intervention_required: ['Needs a person', '需要人工处理'], candidate_declined: ['Declined this work', '未接手这项工作'],
  create_task: ['Created a task', '创建了 Task'], submit_completion: ['Submitted completion', '提交了完成结果'],
  accept_completion: ['Accepted completion', '验收了完成结果'],
}

const reasonNames: Record<string, [string, string]> = {
  requested: ['Stop requested', '收到停止请求'], lease_lost: ['Claim lease lost', 'Claim 租约失效'],
  authority_lost: ['Claim authority lost', '失去 Claim 权限'], runtime_failure: ['Execution failed', '执行失败'],
  protocol_error: ['Invalid execution result', '执行结果无效'], core_rejected: ['Core rejected the result', 'Core 拒绝结果'],
}

type Language = 'en' | 'zh-CN'

function statusLabel(instance: DaemonInstance, locale: Language) {
  return instance.connectivity === 'reporting' && instance.lifecycle === 'stopping' ? copy[locale].daemonStopping : copy[locale][instance.connectivity]
}
function kindLabel(kind: DaemonDispatch['candidate_kind'], locale: Language) {
  const t = copy[locale]
  return { task: t.taskWork, empty_blackboard: t.planning, blackboard_completion: t.completion, work_item_acceptance: t.acceptance }[kind]
}
function stateLabel(state: DaemonDispatch['state'], locale: Language) {
  const t = copy[locale]
  return { prepared: t.prepared, claimed: t.claimed, starting: t.starting, running: t.running,
    finalizing: t.finalizing, stopping: t.stopping, finished: t.finishedState, lost: t.lostState }[state]
}
function claimLabel(status: DaemonDispatch['claim_status'], locale: Language) {
  const t = copy[locale]
  return { not_attempted: t.notAttempted, uncertain: t.uncertain, active: t.active, ended: t.ended }[status]
}
function shortSummary(instance: DaemonInstance, locale: Language) {
  const t = copy[locale]
  if (instance.connectivity !== 'reporting') return instance.active_count === 1 ? t.lastSnapshotOne : instance.active_count > 1 ? t.lastSnapshot.replace('{count}', String(instance.active_count)) : instance.connectivity === 'stale' ? t.staleWorkUnknown : t.stoppedNoWork
  if (instance.lifecycle === 'stopping') return t.stoppingSummary
  if (instance.health === 'paused') return t.reportingPaused
  return instance.active_count === 1 ? t.reportingOne : (instance.active_count > 1 ? t.reportingActive : t.reportingIdle).replace('{count}', String(instance.active_count))
}

export function DaemonsPage({ identity, daemonID, navigate }: {
  identity: Identity; daemonID: string | null; navigate: (route: RouteState, replace?: boolean) => void
}) {
  const { locale, formatTime } = useI18n()
  const t = copy[locale]
  const [includeHistory, setIncludeHistory] = useState(false)
  const instances = useInfiniteQuery({
    queryKey: ['daemons', identity.id, includeHistory], queryFn: ({ pageParam }) => api.listDaemons(identity, pageParam, includeHistory),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: daemonID === null, refetchInterval: daemonID === null ? 15000 : false,
  })
  const rows = useMemo(() => instances.data?.pages.flatMap(page => page.data) ?? [], [instances.data])
  const present = rows.filter(item => item.connectivity !== 'stopped')
  const past = rows.filter(item => item.connectivity === 'stopped')
  const detail = useQuery({
    queryKey: ['daemon', identity.id, daemonID], queryFn: () => api.getDaemon(identity, daemonID!),
    enabled: Boolean(daemonID), refetchInterval: daemonID ? 15000 : false,
  })
  const events = useInfiniteQuery({
    queryKey: ['daemon-events', identity.id, daemonID], queryFn: ({ pageParam }) => api.listDaemonEvents(identity, daemonID!, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: Boolean(daemonID), refetchInterval: daemonID ? 15000 : false,
  })
  const go = (id: string | null) => navigate({ workItemID: null, taskID: null, homeView: 'all', daemonID: id })

  const detailMissing = detail.error instanceof APIError && detail.error.status === 404

  if (daemonID) return <section className="daemon-page daemon-detail-page">
    <button className="back-button" onClick={() => go(null)}><ArrowLeft size={15} />{t.back}</button>
    {detail.isLoading && !detail.data && <p className="daemon-muted">{t.loading}</p>}
    {detail.isError && <p className="daemon-notice" role="status">{!detail.data && detailMissing ? t.missing : t.unavailable}</p>}
    {detail.data && <InstanceDetail instance={detail.data} events={events.data?.pages.flatMap(page => page.data) ?? []}
      eventsLoading={events.isLoading} eventsError={events.isError} hasMoreEvents={Boolean(events.hasNextPage)}
      loadingMoreEvents={events.isFetchingNextPage} loadMoreEvents={() => void events.fetchNextPage()}
      refresh={() => { void detail.refetch(); void events.refetch() }} navigate={navigate} locale={locale} formatTime={formatTime} />}
  </section>

  return <section className="daemon-page daemon-index-page">
    <header className="daemon-page-header"><div><span className="daemon-eyebrow">Daemon</span><h1>{t.title}</h1><p>{t.intro}</p></div><button className="daemon-refresh" aria-label={t.refresh} title={t.refresh} onClick={() => void instances.refetch()}><RefreshCw size={16} /></button></header>
    {instances.isError && <p className="daemon-notice" role="status">{t.unavailable}</p>}
    {instances.isLoading && <p className="daemon-muted">{t.loading}</p>}
    <section className="daemon-index-section"><h2>{t.present}</h2>
      {!instances.isLoading && Boolean(instances.data) && !instances.hasNextPage && present.length === 0 && <p className="daemon-quiet-empty">{t.noPresent}</p>}
      <div className="daemon-card-list">{present.map(item => <button key={item.id} className={`daemon-card ${item.connectivity}`} onClick={() => go(item.id)}>
        <span className="daemon-card-top">
          <span className="daemon-instance-mark" aria-hidden="true"><svg viewBox="0 0 48 48" fill="none">
            <path d="M24 3v6M24 39v6M3 24h6M39 24h6" className="daemon-glyph-trace" />
            <rect x="10" y="10" width="28" height="28" rx="8" className="daemon-glyph-body" />
            <path d="M17 20h13M17 27h8" className="daemon-glyph-line" />
            <circle cx="30" cy="27" r="2" className="daemon-glyph-node" />
          </svg></span>
          <span className="daemon-card-identity"><strong>{item.name || item.agent_id}</strong><Status instance={item} locale={locale} /></span>
          <span className="daemon-card-id">#{item.id.slice(0, 6)}</span>
        </span>
        <span className="daemon-card-summary">{shortSummary(item, locale)}</span>
        <span className="daemon-card-foot"><span>{t.lastSeen.replace('{time}', formatTime(item.last_report_at))}</span><span>{t.choose} <ArrowRight size={14} /></span></span>
      </button>)}</div>
    </section>
    {past.length > 0 && <section className="daemon-index-section daemon-past"><h2>{t.past}</h2><div className="daemon-past-line">{past.map(item => <button className="daemon-past-entry" key={item.id} onClick={() => go(item.id)}><i aria-hidden="true" /><span><strong>{item.name || item.agent_id}</strong><small>{t.lastSeen.replace('{time}', formatTime(item.last_report_at))}</small></span><ArrowRight size={15} /></button>)}</div></section>}
    {instances.hasNextPage && <button className="daemon-more" disabled={instances.isFetchingNextPage} onClick={() => void instances.fetchNextPage()}><ArrowDown size={14} />{t.loadMore}</button>}
    {!instances.isLoading && <button className="daemon-history-toggle" onClick={() => setIncludeHistory(value => !value)}>{includeHistory ? t.recentOnly : t.older}</button>}
  </section>
}

function Status({ instance, locale }: { instance: DaemonInstance; locale: Language }) {
  return <span className={`daemon-status ${instance.connectivity}`}><i aria-hidden="true" />{statusLabel(instance, locale)}</span>
}

function InstanceDetail({ instance, events, eventsLoading, eventsError, hasMoreEvents, loadingMoreEvents, loadMoreEvents, refresh, navigate, locale, formatTime }: {
  instance: DaemonInstance; events: DaemonEvent[]; eventsLoading: boolean; eventsError: boolean
  hasMoreEvents: boolean; loadingMoreEvents: boolean; loadMoreEvents: () => void; refresh: () => void
  navigate: (route: RouteState) => void; locale: Language; formatTime: (value: string) => string
}) {
  const t = copy[locale]
  const last = formatTime(instance.last_report_at)
  const summary = instance.connectivity === 'stale' ? t.staleSummary.replace('{time}', last)
    : instance.connectivity === 'stopped' ? t.stoppedSummary.replace('{time}', last) : shortSummary(instance, locale)
  const current = instance.connectivity === 'reporting'
  return <>
    <header className="daemon-detail-hero"><div><span className="daemon-eyebrow">Daemon</span><h1>{instance.name || instance.agent_id}</h1><Status instance={instance} locale={locale} /><p>{summary}</p></div><button className="daemon-refresh" aria-label={t.refresh} title={t.refresh} onClick={refresh}><RefreshCw size={16} /></button></header>
    <section className="daemon-content-section"><h2>{current ? t.current : t.lastWork}</h2>
      {instance.active_dispatches.length === 0 && <p className="daemon-quiet-empty">{current ? t.noCurrent : t.noLastWork}</p>}
      <div className="daemon-dispatch-list">{instance.active_dispatches.map(dispatch => <article className="daemon-dispatch" key={dispatch.id}>
        <div className="daemon-dispatch-heading"><h3>{kindLabel(dispatch.candidate_kind, locale)}</h3><span>{stateLabel(dispatch.state, locale)}</span></div>
        <p>{claimLabel(dispatch.claim_status, locale)} · {t.attempt.replace('{count}', String(dispatch.attempts))}</p>
        <button className="daemon-inline-link" onClick={() => navigate({ workItemID: dispatch.work_item_id, taskID: dispatch.task_id, homeView: 'all' })}>{t.openWork}<ArrowUpRight size={14} /></button>
      </article>)}</div>
      {instance.omitted_active_count > 0 && <p className="daemon-muted">{t.omitted.replace('{count}', String(instance.omitted_active_count))}</p>}
    </section>
    <section className="daemon-content-section"><h2>{t.activity}</h2>
      <p className="daemon-muted">{t.retention}</p>
      {instance.dropped_events > 0 && <p className="daemon-notice">{t.gap.replace('{count}', String(instance.dropped_events))}</p>}
      {eventsError && <p className="daemon-notice" role="status">{t.unavailable}</p>}
      {eventsLoading && <p className="daemon-muted">{t.loadingActivity}</p>}
      {!eventsLoading && !eventsError && events.length === 0 && <p className="daemon-quiet-empty">{t.noActivity}</p>}
      {events.length > 0 && <div className="daemon-timeline">{events.map(event => <article className="daemon-timeline-entry" key={event.sequence}><i aria-hidden="true" /><div><div className="daemon-event-heading"><h3>{eventNames[event.kind]?.[locale === 'en' ? 0 : 1] ?? event.kind.replaceAll('_', ' ')}</h3><time>{formatTime(event.occurred_at)}</time></div>{eventSummary(event, locale) && <p>{eventSummary(event, locale)}</p>}{event.work_item_id && <button className="daemon-inline-link" onClick={() => navigate({ workItemID: event.work_item_id, taskID: event.task_id, homeView: 'all' })}>{t.openWork}<ArrowUpRight size={13} /></button>}</div></article>)}</div>}
      {hasMoreEvents && <button className="daemon-more" disabled={loadingMoreEvents} onClick={loadMoreEvents}><ArrowDown size={14} />{t.loadMore}</button>}
    </section>
    <details className="daemon-technical"><summary>{t.details}</summary><dl>
      <Technical label={t.agent} value={instance.agent_id} /><Technical label={t.instance} value={instance.id} />
      <Technical label={t.adapter} value={instance.adapter} /><Technical label={t.version} value={instance.daemon_version} />
      <Technical label={t.slots} value={instance.slots} /><Technical label={t.health} value={t[instance.health]} />
      <Technical label={t.started} value={formatTime(instance.process_started_at)} />
      <Technical label={t.observed} value={formatTime(instance.as_of)} /><Technical label={t.dropped} value={instance.dropped_events} />
    </dl></details>
  </>
}

function Technical({ label, value }: { label: string; value: string | number }) { return <div><dt>{label}</dt><dd>{value}</dd></div> }

function eventSummary(event: DaemonEvent, locale: Language) {
  const t = copy[locale]
  const side = locale === 'en' ? 0 : 1
  const state = event.details.state && ['prepared', 'claimed', 'starting', 'running', 'finalizing', 'stopping', 'finished', 'lost'].includes(event.details.state)
    ? stateLabel(event.details.state as DaemonDispatch['state'], locale) : event.details.state
  return [event.candidate_kind ? kindLabel(event.candidate_kind, locale) : null, state,
    event.details.attempts !== undefined ? t.attempt.replace('{count}', String(event.details.attempts)) : null,
    event.details.outcome ? outcomeNames[event.details.outcome]?.[side] ?? event.details.outcome : null,
    event.details.applied === true ? t.outcomeApplied : event.details.applied === false && event.details.outcome ? t.outcomeUnconfirmed : null,
    event.details.duration_ms !== undefined ? t.duration.replace('{value}', String(event.details.duration_ms)) : null,
    event.details.reason ? reasonNames[event.details.reason]?.[side] ?? event.details.reason : null].filter(Boolean).join(' · ')
}
