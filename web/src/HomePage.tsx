import { useMemo } from 'react'
import { ArrowDown, ArrowRight, Check, CircleDot, XCircle } from 'lucide-react'
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
  const { activeWorkItems, settledWorkItems, attention } = useHomeData(identity, true)
  const activeWork = useMemo(() => activeWorkItems.data?.pages.flatMap(page => page.data) ?? [], [activeWorkItems.data])
  const settledWork = useMemo(() => (settledWorkItems.data?.pages.flatMap(page => page.data) ?? []).sort((left, right) => new Date(right.completed_at ?? right.updated_at).getTime() - new Date(left.completed_at ?? left.updated_at).getTime()), [settledWorkItems.data])
  const humanAttention = useMemo(() => attention.data?.pages.flatMap(page => page.data) ?? [], [attention.data])
  const error = activeWorkItems.error ?? settledWorkItems.error ?? attention.error
  const workIsLoading = activeWorkItems.isLoading || settledWorkItems.isLoading

  return <section className="queue-panel mobile-queue">
    {error && <div className="error-banner"><XCircle size={16} /><span>{error instanceof APIError ? error.message : t('unreachable')}</span></div>}
    <div className="welcome"><span>{t('workspace')}</span><h1>{t('welcomeTitle')}</h1><p>{t('welcomeBody')}</p></div>
    <div className="home-tabs" role="tablist" aria-label={t('workspaceViews')}><button role="tab" aria-selected={homeView === 'all'} className={homeView === 'all' ? 'active' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all' })}>{t('allWork')}</button><button role="tab" aria-selected={homeView === 'human'} className={homeView === 'human' ? 'active' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'human' })}>{t('needsHuman')}</button></div>
    {homeView === 'all' && <><div className="work-list">
      {workIsLoading && <LoadingRows />}
      {!workIsLoading && activeWork.length === 0 && settledWork.length === 0 && <EmptyState title={t('quiet')} action={onCreate} />}
      {activeWork.map(item => <WorkEntry key={item.id} item={item} onOpen={() => navigate({ workItemID: item.id, taskID: null, homeView })} />)}
    </div>
    {activeWorkItems.hasNextPage && <button className="load-more-button" disabled={activeWorkItems.isFetchingNextPage} onClick={() => activeWorkItems.fetchNextPage()}><ArrowDown size={15} />{t(activeWorkItems.isFetchingNextPage ? 'loadingMore' : 'loadMoreActiveWork')}</button>}
    {settledWork.length > 0 && <section className="work-history"><div className="history-heading"><h2>{t('workAtRest')}</h2><p>{t('historyBody')}</p></div><div className="history-line">
      {settledWork.map(item => <button className="history-entry" key={item.id} onClick={() => navigate({ workItemID: item.id, taskID: null, homeView })}><i aria-hidden="true" /><div className="history-content"><div><strong>{item.title}</strong><Status value={item.status} /></div><p>{item.goal}</p><dl><div><dt>{t('started')}</dt><dd>{formatDate(item.created_at)}</dd></div><div><dt>{t(item.status === 'completed' ? 'completed' : 'closed')}</dt><dd>{formatDate(item.completed_at ?? item.updated_at)}</dd></div></dl></div></button>)}
    </div>{settledWorkItems.hasNextPage && <button className="load-more-button" disabled={settledWorkItems.isFetchingNextPage} onClick={() => settledWorkItems.fetchNextPage()}><ArrowDown size={15} />{t(settledWorkItems.isFetchingNextPage ? 'loadingMore' : 'loadMoreHistory')}</button>}</section>}</>}
    {homeView === 'human' && <div className="attention-list">
      {attention.isLoading && humanAttention.length === 0 && <LoadingRows />}
      {!attention.isLoading && humanAttention.length === 0 && <div className="attention-empty"><Check size={22} /><strong>{t('nothingNeedsYou')}</strong><p>{t('nothingNeedsYouBody')}</p></div>}
      {humanAttention.map(item => <button className="attention-entry" key={`${item.kind}-${item.work_item.id}-${item.task?.id ?? ''}`} onClick={() => navigate({ workItemID: item.work_item.id, taskID: item.task?.id ?? null, homeView: 'human' })}><div><span>{item.work_item.title}</span><strong>{item.task?.title ?? item.work_item.goal}</strong></div><span className={`attention-kind ${item.kind === 'review' ? 'review' : 'human'}`}>{t(item.kind === 'review' ? 'reviewNeeded' : item.kind === 'work_item_acceptance' ? 'workAcceptanceNeeded' : 'humanTask')}</span><ArrowRight size={17} /></button>)}
      {attention.hasNextPage && <button className="load-more-button" disabled={attention.isFetchingNextPage} onClick={() => attention.fetchNextPage()}><ArrowDown size={15} />{t(attention.isFetchingNextPage ? 'loadingMore' : 'loadMore')}</button>}
    </div>}
  </section>
}

function WorkEntry({ item, onOpen }: { item: WorkItem; onOpen: () => void }) {
  const { t } = useI18n()
  return <button className="work-row" onClick={onOpen}><div className="row-main"><div className="row-title"><strong>{item.title}</strong><Status value={item.status} /></div><p>{item.goal}</p><span className="enter-work">{t('openWorkspace')} <ArrowRight size={14} /></span></div></button>
}

function LoadingRows() { return <>{[0, 1, 2].map(item => <div className="loading-row" key={item}><i /><span /></div>)}</> }
function EmptyState({ title, action }: { title: string; action: () => void }) { const { t } = useI18n(); return <div className="empty-state"><CircleDot size={24} /><strong>{title}</strong><button onClick={action}>{t('createWork')}</button></div> }
