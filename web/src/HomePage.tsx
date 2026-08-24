import { useMemo } from 'react'
import { ArrowRight, Check, CircleDot, XCircle } from 'lucide-react'
import { APIError } from './api'
import { useI18n } from './i18n'
import { useHomeData } from './pageData'
import type { HomeView, RouteState } from './route'
import type { Identity, WorkItem } from './types'
import { Status } from './ui'

export function HomePage({ identity, homeView, navigate, onCreate }: {
  identity: Identity; homeView: HomeView; navigate: (route: RouteState) => void; onCreate: () => void
}) {
  const { t, formatDate } = useI18n()
  const { workItems, attention } = useHomeData(identity, true)
  const orderedWork = useMemo(() => [...(workItems.data ?? [])].sort((left, right) => {
    const priority = (status: string) => status === 'open' ? 0 : 1
    return priority(left.status) - priority(right.status)
  }), [workItems.data])
  const activeWork = orderedWork.filter(item => item.status === 'open' || item.status === 'awaiting_agent_acceptance' || item.status === 'awaiting_human_acceptance')
  const settledWork = orderedWork.filter(item => item.status !== 'open' && item.status !== 'awaiting_agent_acceptance' && item.status !== 'awaiting_human_acceptance').sort((left, right) => new Date(right.completed_at ?? right.updated_at).getTime() - new Date(left.completed_at ?? left.updated_at).getTime())
  const humanAttention = attention.data ?? []
  const error = workItems.error ?? attention.error

  return <section className="queue-panel mobile-queue">
    {error && <div className="error-banner"><XCircle size={16} /><span>{error instanceof APIError ? error.message : t('unreachable')}</span></div>}
    <div className="welcome"><span>{t('workspace')}</span><h1>{t('welcomeTitle')}</h1><p>{t('welcomeBody')}</p></div>
    <div className="home-tabs" role="tablist" aria-label={t('workspaceViews')}><button role="tab" aria-selected={homeView === 'all'} className={homeView === 'all' ? 'active' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all' })}>{t('allWork')}</button><button role="tab" aria-selected={homeView === 'human'} className={homeView === 'human' ? 'active' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'human' })}>{t('needsHuman')}{humanAttention.length > 0 && <span>{humanAttention.length}</span>}</button></div>
    {homeView === 'all' && <><div className="work-list">
      {workItems.isLoading && <LoadingRows />}
      {!workItems.isLoading && orderedWork.length === 0 && <EmptyState title={t('quiet')} action={onCreate} />}
      {activeWork.map(item => <WorkEntry key={item.id} item={item} onOpen={() => navigate({ workItemID: item.id, taskID: null, homeView })} />)}
    </div>
    {settledWork.length > 0 && <section className="work-history"><div className="history-heading"><h2>{t('workAtRest')}</h2><p>{t('historyBody')}</p></div><div className="history-line">
      {settledWork.map(item => <button className="history-entry" key={item.id} onClick={() => navigate({ workItemID: item.id, taskID: null, homeView })}><i aria-hidden="true" /><div className="history-content"><div><strong>{item.title}</strong><Status value={item.status} /></div><p>{item.goal}</p><dl><div><dt>{t('started')}</dt><dd>{formatDate(item.created_at)}</dd></div><div><dt>{t(item.status === 'completed' ? 'completed' : 'closed')}</dt><dd>{formatDate(item.completed_at ?? item.updated_at)}</dd></div></dl></div></button>)}
    </div></section>}</>}
    {homeView === 'human' && <div className="attention-list">
      {attention.isLoading && humanAttention.length === 0 && <LoadingRows />}
      {!attention.isLoading && humanAttention.length === 0 && <div className="attention-empty"><Check size={22} /><strong>{t('nothingNeedsYou')}</strong><p>{t('nothingNeedsYouBody')}</p></div>}
      {humanAttention.map(item => <button className="attention-entry" key={`${item.kind}-${item.work_item.id}-${item.task?.id ?? ''}`} onClick={() => navigate({ workItemID: item.work_item.id, taskID: item.task?.id ?? null, homeView: 'human' })}><div><span>{item.work_item.title}</span><strong>{item.task?.title ?? item.work_item.goal}</strong></div><span className={`attention-kind ${item.kind === 'review' ? 'review' : 'human'}`}>{t(item.kind === 'review' ? 'reviewNeeded' : item.kind === 'work_item_acceptance' ? 'workAcceptanceNeeded' : 'humanTask')}</span><ArrowRight size={17} /></button>)}
    </div>}
  </section>
}

function WorkEntry({ item, onOpen }: { item: WorkItem; onOpen: () => void }) {
  const { t } = useI18n()
  return <button className="work-row" onClick={onOpen}><div className="row-main"><div className="row-title"><strong>{item.title}</strong><Status value={item.status} /></div><p>{item.goal}</p><span className="enter-work">{t('openWorkspace')} <ArrowRight size={14} /></span></div></button>
}

function LoadingRows() { return <>{[0, 1, 2].map(item => <div className="loading-row" key={item}><i /><span /></div>)}</> }
function EmptyState({ title, action }: { title: string; action: () => void }) { const { t } = useI18n(); return <div className="empty-state"><CircleDot size={24} /><strong>{title}</strong><button onClick={action}>{t('createWork')}</button></div> }
