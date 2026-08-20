import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { GitBranch, Languages, Library, Plus, UserRound } from 'lucide-react'
import { loadIdentity, saveIdentity } from './api'
import { CreateWorkModal, IdentityModal, type WorkDefinitionTarget } from './AppModals'
import { HomePage } from './HomePage'
import { useI18n } from './i18n'
import { readRoute, routePath, type RouteState } from './route'
import type { Identity } from './types'

const BlackboardsPage = lazy(() => import('./BlackboardsPage').then(module => ({ default: module.BlackboardsPage })))
const WorkItemPage = lazy(() => import('./WorkItemPage').then(module => ({ default: module.WorkItemPage })))
const WorkflowsPage = lazy(() => import('./WorkflowsPage').then(module => ({ default: module.WorkflowsPage })))
const WorkflowEditorPage = lazy(() => import('./WorkflowEditorPage').then(module => ({ default: module.WorkflowEditorPage })))

export function App() {
  const { locale, setLocale, t } = useI18n()
  const initialRoute = useMemo(() => readRoute(window.location.pathname), [])
  const [identity, setIdentity] = useState(loadIdentity)
  const [route, setRoute] = useState(initialRoute)
  const [createOpen, setCreateOpen] = useState(false)
  const [createDefinition, setCreateDefinition] = useState<WorkDefinitionTarget | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)

  function navigate(next: RouteState, replace = false) {
    const path = routePath(next)
    if (window.location.pathname !== path) window.history[replace ? 'replaceState' : 'pushState']({}, '', path)
    setRoute(next)
  }

  useEffect(() => {
    const restoreRoute = () => setRoute(readRoute(window.location.pathname))
    window.addEventListener('popstate', restoreRoute)
    return () => window.removeEventListener('popstate', restoreRoute)
  }, [])

  function updateIdentity(next: Identity) {
    saveIdentity(next)
    setIdentity(next)
    navigate({ workItemID: null, taskID: null, homeView: 'all' })
    setSettingsOpen(false)
  }

  return <div className="app-shell">
    <header className="topbar">
      <button className="brand" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all' })}><div className="brand-mark"><span>K</span></div><strong>Kairos</strong></button>
      <div className="top-actions">
        <button className={`library-link ${route.blackboardID !== undefined ? 'active' : ''}`} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: null })}><Library size={16} />{t('blackboards')}</button>
        <button className={`library-link ${route.workflowID !== undefined ? 'active' : ''}`} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: null })}><GitBranch size={16} />{t('workflows')}</button>
        <button className="language-button" onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')} aria-label={locale === 'en' ? '切换到中文' : 'Switch to English'}><Languages size={16} /><span>{locale === 'en' ? '中文' : 'EN'}</span></button>
        <button className="icon-button" onClick={() => setSettingsOpen(true)} aria-label={t('identitySettings')} title={`${t('identity')}: ${identity.id}`}><UserRound size={17} /></button>
        {!route.workItemID && route.blackboardID === undefined && route.workflowID === undefined && <button className="primary-button" onClick={() => setCreateOpen(true)}><Plus size={17} />{t('startSomething')}</button>}
      </div>
    </header>

    <main className={`workspace ${route.blackboardID !== undefined || route.workflowID !== undefined ? 'library-workspace' : ''} ${route.workItemID ? 'show-work' : 'show-queue'} ${route.taskID ? 'task-open' : ''}`}><Suspense fallback={<div className="panel-placeholder"><strong>{t('acquiring')}</strong></div>}>
      {route.workflowID !== undefined
        ? route.workflowEditing
          ? <WorkflowEditorPage identity={identity} workflowID={route.workflowID ?? null} workflowVersion={route.workflowVersion ?? null} navigate={navigate} />
          : <WorkflowsPage identity={identity} workflowID={route.workflowID ?? null} workflowVersion={route.workflowVersion ?? null} navigate={navigate} onStartWork={definition => { setCreateDefinition(definition); setCreateOpen(true) }} />
        : route.blackboardID !== undefined
        ? <BlackboardsPage identity={identity} blackboardID={route.blackboardID ?? null} blackboardVersion={route.blackboardVersion ?? null} navigate={navigate} onStartWork={definition => { setCreateDefinition(definition); setCreateOpen(true) }} />
        : !route.workItemID
        ? <HomePage identity={identity} homeView={route.homeView} navigate={navigate} onCreate={() => setCreateOpen(true)} />
        : <WorkItemPage identity={identity} workItemID={route.workItemID} selectedTaskID={route.taskID} homeView={route.homeView} navigate={navigate} />}
      </Suspense>
    </main>

    <CreateWorkModal open={createOpen} onOpenChange={open => { setCreateOpen(open); if (!open) setCreateDefinition(null) }} identity={identity} definition={createDefinition} onCreated={workItem => { if (createDefinition) navigate({ workItemID: workItem.ID, taskID: null, homeView: 'all' }) }} />
    <IdentityModal open={settingsOpen} onOpenChange={setSettingsOpen} identity={identity} onSave={updateIdentity} />
  </div>
}
