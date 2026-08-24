import { lazy, Suspense, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { GitBranch, KeyRound, Languages, Library, LoaderCircle, LogOut, Plus, RefreshCw, UserRound } from 'lucide-react'
import { APIError, api, authenticationRequiredEvent, clearBearerToken, configureAuthenticationMode, loadBearerToken, loadIdentity, saveBearerToken, saveIdentity, tokenStorageUnavailableEvent, TokenStorageError } from './api'
import { CreateWorkModal, IdentityModal, type WorkDefinitionTarget } from './AppModals'
import { HomePage } from './HomePage'
import { useI18n } from './i18n'
import { readRoute, routePath, type RouteState } from './route'
import type { AuthenticationMode, Identity } from './types'

const BlackboardsPage = lazy(() => import('./BlackboardsPage').then(module => ({ default: module.BlackboardsPage })))
const WorkItemPage = lazy(() => import('./WorkItemPage').then(module => ({ default: module.WorkItemPage })))
const WorkflowsPage = lazy(() => import('./WorkflowsPage').then(module => ({ default: module.WorkflowsPage })))
const WorkflowEditorPage = lazy(() => import('./WorkflowEditorPage').then(module => ({ default: module.WorkflowEditorPage })))

type AuthenticationState =
  | { status: 'loading' }
  | { status: 'error'; source: 'config' | 'session' | 'storage' }
  | { status: 'login'; error?: AuthenticationError }
  | { status: 'ready'; mode: AuthenticationMode; identity: Identity }

type AuthenticationError = 'invalidToken' | 'authenticationFailed' | 'sessionExpired'

export function App() {
  const queryClient = useQueryClient()
  const [authentication, setAuthentication] = useState<AuthenticationState>({ status: 'loading' })
  const [bootstrapAttempt, setBootstrapAttempt] = useState(0)

  useEffect(() => {
    let active = true
    async function bootstrap() {
      setAuthentication({ status: 'loading' })
      let mode: AuthenticationMode
      try {
        const config = await api.getAuthenticationConfig()
        mode = config.mode
      } catch {
        if (active) setAuthentication({ status: 'error', source: 'config' })
        return
      }
      if (!active) return
      configureAuthenticationMode(mode)
      if (mode === 'trusted') {
        try {
          clearBearerToken()
        } catch { /* Trusted transport does not depend on Token storage. */ }
        setAuthentication({ status: 'ready', mode, identity: loadIdentity() })
        return
      }
      let token: string
      try {
        token = loadBearerToken()
      } catch (error) {
        if (error instanceof TokenStorageError) setAuthentication({ status: 'error', source: 'storage' })
        return
      }
      if (!token) {
        setAuthentication({ status: 'login' })
        return
      }
      try {
        const identity = await api.getSession()
        if (active) setAuthentication({ status: 'ready', mode, identity })
      } catch (error) {
        if (!active) return
        if (error instanceof TokenStorageError) {
          setAuthentication({ status: 'error', source: 'storage' })
          return
        }
        if (error instanceof APIError && error.status === 401) {
          try {
            clearBearerToken()
          } catch {
            setAuthentication({ status: 'error', source: 'storage' })
            return
          }
          setAuthentication({ status: 'login', error: 'sessionExpired' })
          return
        }
        setAuthentication({ status: 'error', source: 'session' })
      }
    }
    void bootstrap()
    return () => { active = false }
  }, [bootstrapAttempt])

  useEffect(() => {
    const requireAuthentication = () => {
      queryClient.clear()
      try {
        clearBearerToken()
      } catch {
        setAuthentication({ status: 'error', source: 'storage' })
        return
      }
      setAuthentication(current => current.status === 'ready' && current.mode === 'authenticated'
        ? { status: 'login', error: 'sessionExpired' }
        : current)
    }
    window.addEventListener(authenticationRequiredEvent, requireAuthentication)
    const reportStorageUnavailable = () => {
      queryClient.clear()
      setAuthentication({ status: 'error', source: 'storage' })
    }
    window.addEventListener(tokenStorageUnavailableEvent, reportStorageUnavailable)
    return () => {
      window.removeEventListener(authenticationRequiredEvent, requireAuthentication)
      window.removeEventListener(tokenStorageUnavailableEvent, reportStorageUnavailable)
    }
  }, [queryClient])

  async function login(token: string) {
    try {
      saveBearerToken(token)
      const identity = await api.getSession()
      queryClient.clear()
      setAuthentication({ status: 'ready', mode: 'authenticated', identity })
    } catch (error) {
      if (error instanceof TokenStorageError) {
        setAuthentication({ status: 'error', source: 'storage' })
        return
      }
      try {
        clearBearerToken()
      } catch {
        setAuthentication({ status: 'error', source: 'storage' })
        return
      }
      const message = error instanceof APIError && error.status === 401 ? 'invalidToken' : 'authenticationFailed'
      setAuthentication({ status: 'login', error: message })
    }
  }

  function logout() {
    queryClient.clear()
    try {
      clearBearerToken()
    } catch {
      setAuthentication({ status: 'error', source: 'storage' })
      return
    }
    setAuthentication({ status: 'login' })
  }

  if (authentication.status === 'loading') return <AuthenticationPage mode="loading" />
  if (authentication.status === 'error') return <AuthenticationPage mode="error" errorSource={authentication.source} onRetry={() => setBootstrapAttempt(value => value + 1)} />
  if (authentication.status === 'login') return <TokenLogin error={authentication.error} onLogin={login} />
  return <ConsoleApp identity={authentication.identity} authenticationMode={authentication.mode} onLogout={logout} />
}

function AuthenticationPage({ mode, errorSource, onRetry }: { mode: 'loading' | 'error'; errorSource?: 'config' | 'session' | 'storage'; onRetry?: () => void }) {
  const { locale, setLocale, t } = useI18n()
  return <div className="auth-shell">
    <header className="auth-header">
      <div className="auth-brand"><div className="brand-mark"><span>K</span></div><strong>Kairos</strong></div>
      <button className="language-button" onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')} aria-label={locale === 'en' ? '切换到中文' : 'Switch to English'}><Languages size={16} /><span>{locale === 'en' ? '中文' : 'EN'}</span></button>
    </header>
    <main className="auth-main">
      {mode === 'loading'
        ? <div className="auth-status" role="status"><LoaderCircle className="spin" size={22} /><strong>{t('checkingAuthentication')}</strong></div>
        : <div className="auth-status"><strong>{t('consoleUnavailable')}</strong><p>{t(errorSource === 'storage' ? 'tokenStorageUnavailableBody' : errorSource === 'session' ? 'sessionUnavailableBody' : 'consoleUnavailableBody')}</p><button className="primary-button auth-retry" onClick={onRetry}><RefreshCw size={16} />{t('retry')}</button></div>}
    </main>
  </div>
}

function TokenLogin({ error, onLogin }: { error?: AuthenticationError; onLogin: (token: string) => Promise<void> }) {
  const { locale, setLocale, t } = useI18n()
  const [token, setToken] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    const value = token.trim()
    if (!value || submitting) return
    setSubmitting(true)
    try {
      await onLogin(value)
    } finally {
      setSubmitting(false)
    }
  }

  return <div className="auth-shell">
    <header className="auth-header">
      <div className="auth-brand"><div className="brand-mark"><span>K</span></div><strong>Kairos</strong></div>
      <button className="language-button" onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')} aria-label={locale === 'en' ? '切换到中文' : 'Switch to English'}><Languages size={16} /><span>{locale === 'en' ? '中文' : 'EN'}</span></button>
    </header>
    <main className="auth-main">
      <form className="token-login" onSubmit={submit}>
        <div className="token-login-icon"><KeyRound size={22} /></div>
        <h1>{t('tokenLoginTitle')}</h1>
        <p>{t('tokenLoginBody')}</p>
        <label htmlFor="identity-token">{t('identityToken')}</label>
        <input id="identity-token" type="password" autoComplete="off" autoFocus value={token} onChange={event => setToken(event.target.value)} placeholder={t('identityTokenPlaceholder')} aria-invalid={Boolean(error)} />
        {error && <div className="auth-error" role="alert">{t(error)}</div>}
        <button className="primary-button token-submit" type="submit" disabled={!token.trim() || submitting}>{submitting ? <LoaderCircle className="spin" size={16} /> : <KeyRound size={16} />}{submitting ? t('authenticating') : t('signIn')}</button>
      </form>
    </main>
  </div>
}

function ConsoleApp({ identity: initialIdentity, authenticationMode, onLogout }: { identity: Identity; authenticationMode: AuthenticationMode; onLogout: () => void }) {
  const { locale, setLocale, t } = useI18n()
  const initialRoute = useMemo(() => readRoute(window.location.pathname), [])
  const [identity, setIdentity] = useState(initialIdentity)
  const [route, setRoute] = useState(initialRoute)
  const [createOpen, setCreateOpen] = useState(false)
  const [createDefinition, setCreateDefinition] = useState<WorkDefinitionTarget | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const accountMenuRef = useRef<HTMLDivElement>(null)
  const accountTriggerRef = useRef<HTMLButtonElement>(null)

  function navigate(next: RouteState, replace = false) {
    setAccountOpen(false)
    const path = routePath(next)
    if (window.location.pathname !== path) window.history[replace ? 'replaceState' : 'pushState']({}, '', path)
    setRoute(next)
  }

  useEffect(() => {
    const restoreRoute = () => {
      setAccountOpen(false)
      setRoute(readRoute(window.location.pathname))
    }
    window.addEventListener('popstate', restoreRoute)
    return () => window.removeEventListener('popstate', restoreRoute)
  }, [])

  useEffect(() => {
    if (!accountOpen) return
    const closeOutside = (event: PointerEvent) => {
      if (!accountMenuRef.current?.contains(event.target as Node)) setAccountOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setAccountOpen(false)
      accountTriggerRef.current?.focus()
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [accountOpen])

  function updateIdentity(next: Identity) {
    saveIdentity(next)
    setIdentity(next)
    navigate({ workItemID: null, taskID: null, homeView: 'all' })
    setSettingsOpen(false)
  }

  return <div className="app-shell">
    <header className="topbar">
      <button className="brand" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all' })}><div className="brand-mark"><span>K</span></div><strong>Kairos</strong></button>
      <nav className="top-nav" aria-label={t('blackboardLibrary')}>
        <button className={`library-link ${route.blackboardID !== undefined ? 'active' : ''}`} title={t('blackboards')} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: null })}><Library size={16} />{t('blackboards')}</button>
        <button className={`library-link ${route.workflowID !== undefined ? 'active' : ''}`} title={t('workflows')} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: null })}><GitBranch size={16} />{t('workflows')}</button>
      </nav>
      <div className="top-actions">
        <button className="language-button" onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')} aria-label={locale === 'en' ? '切换到中文' : 'Switch to English'}><Languages size={16} /><span>{locale === 'en' ? '中文' : 'EN'}</span></button>
        {authenticationMode === 'trusted'
          ? <button className="icon-button" onClick={() => setSettingsOpen(true)} aria-label={t('identitySettings')} title={`${t('identity')}: ${identity.id}`}><UserRound size={17} /></button>
          : <div className="account-menu" ref={accountMenuRef}><button ref={accountTriggerRef} className="icon-button account-trigger" aria-label={`${t('authenticatedAs')}: ${identity.id}`} title={`${t('authenticatedAs')}: ${identity.id}`} aria-controls={accountOpen ? 'account-popover' : undefined} aria-expanded={accountOpen} onClick={() => setAccountOpen(open => !open)}><UserRound size={17} /></button>{accountOpen && <div id="account-popover" className="account-popover"><div className="account-identity"><span>{t('authenticatedAs')}</span><strong>{identity.id}</strong>{identity.role && <small>{identity.role}</small>}</div><button onClick={onLogout}><LogOut size={15} />{t('logout')}</button></div>}</div>}
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
    {authenticationMode === 'trusted' && <IdentityModal open={settingsOpen} onOpenChange={setSettingsOpen} identity={identity} onSave={updateIdentity} />}
  </div>
}
