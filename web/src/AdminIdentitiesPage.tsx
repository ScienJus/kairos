import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { Languages } from 'lucide-react'
import { APIError } from './api'
import { createIdentity, verifyAdmin } from './adminApi'
import { useI18n } from './i18n'
import type { CreateIdentityInput, IssuedIdentityToken } from './types'

export function AdminIdentitiesPage() {
  const { locale, setLocale, t } = useI18n()
  const credential = useRef('')
  const request = useRef<AbortController | null>(null)
  const generation = useRef(0)
  const [token, setToken] = useState('')
  const [authenticated, setAuthenticated] = useState(false)
  const [busy, setBusy] = useState(false)
  const [id, setID] = useState('')
  const [kind, setKind] = useState<CreateIdentityInput['kind']>('human')
  const [role, setRole] = useState('')
  const [issued, setIssued] = useState<IssuedIdentityToken | null>(null)
  const [error, setError] = useState<'adminRejected' | 'adminUnavailable' | 'adminInvalid' | 'adminConflict' | 'adminUncertain' | null>(null)
  const [copyStatus, setCopyStatus] = useState<'adminCopied' | 'adminCopyFailed' | null>(null)

  const invalidateRequests = useCallback(() => {
    generation.current++
    request.current?.abort()
    request.current = null
    credential.current = ''
  }, [])

  const clearSession = useCallback(() => {
    invalidateRequests()
    setToken(''); setAuthenticated(false); setBusy(false)
    setID(''); setKind('human'); setRole(''); setIssued(null); setCopyStatus(null); setError(null)
  }, [invalidateRequests])

  useEffect(() => {
    // pagehide also clears a document restored from the browser back/forward cache.
    const onPageHide = () => flushSync(clearSession)
    window.addEventListener('pagehide', onPageHide)
    return () => {
      window.removeEventListener('pagehide', onPageHide)
      invalidateRequests()
    }
  }, [clearSession, invalidateRequests])

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (request.current) return
    if (authenticated ? !id.trim() || (kind === 'agent' && !role.trim()) : !token.trim()) return
    const controller = new AbortController()
    request.current = controller
    const current = ++generation.current
    setBusy(true); setError(null); setIssued(null); setCopyStatus(null)
    const authenticating = !authenticated
    const submittedToken = authenticating ? token.trim() : credential.current
    setToken('')
    try {
      if (authenticating) {
        await verifyAdmin(submittedToken, controller.signal)
        if (current !== generation.current) return
        credential.current = submittedToken
        setAuthenticated(true)
      } else {
        const result = await createIdentity(submittedToken, { id, kind, role: kind === 'agent' ? role.trim() : '' }, controller.signal)
        if (current !== generation.current) return
        setIssued(result); setID(''); setRole('')
      }
    } catch (cause) {
      if (current !== generation.current) return
      if (cause instanceof APIError && cause.status === 401) {
        clearSession()
        setError('adminRejected')
      } else if (authenticating) setError('adminUnavailable')
      else if (cause instanceof APIError && cause.status === 409) setError('adminConflict')
      else if (cause instanceof APIError && cause.status === 400) setError('adminInvalid')
      else setError('adminUncertain')
    } finally {
      if (current === generation.current) { request.current = null; setBusy(false) }
    }
  }

  async function copyToken() {
    if (!issued) return
    const current = generation.current
    try {
      await navigator.clipboard.writeText(issued.token)
      if (current === generation.current) setCopyStatus('adminCopied')
    } catch {
      if (current === generation.current) setCopyStatus('adminCopyFailed')
    }
  }

  function dismissResult() {
    generation.current++
    setIssued(null); setCopyStatus(null)
  }

  return <div className="auth-shell">
    <header className="auth-header">
      <a className="auth-brand" href="/" onClick={clearSession}><img className="brand-mark" src="/kairos-logo-mark.png" alt="" /><strong>Kairos</strong></a>
      <button className="language-button" onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')} aria-label={locale === 'en' ? '切换到中文' : 'Switch to English'}><Languages size={16} /><span>{locale === 'en' ? '中文' : 'EN'}</span></button>
    </header>
    <main className="auth-main">
      <section className="token-login admin-panel">
        <h1>{t('adminIdentities')}</h1>
        <p>{t('adminBody')}</p>
        <form className="admin-form" onSubmit={submit}>
          {!authenticated ? <><label htmlFor="admin-token">Admin Token</label><input id="admin-token" type="password" autoComplete="off" autoFocus value={token} disabled={busy} onChange={event => setToken(event.target.value)} /><button className="primary-button token-submit" disabled={busy || !token.trim()}>{busy ? t('authenticating') : t('adminConnect')}</button></>
            : <>
              <label htmlFor="new-identity-id">{t('adminID')}</label><input id="new-identity-id" value={id} disabled={busy} autoComplete="off" onChange={event => setID(event.target.value)} />
              <label htmlFor="new-identity-kind">{t('adminKind')}</label><select id="new-identity-kind" value={kind} disabled={busy} onChange={event => { setKind(event.target.value as CreateIdentityInput['kind']); setRole('') }}><option value="human">Human</option><option value="agent">Agent</option></select>
              {kind === 'agent' && <><label htmlFor="new-identity-role">{t('adminRole')}</label><input id="new-identity-role" value={role} disabled={busy} placeholder="developer" onChange={event => setRole(event.target.value)} /></>}
              <button className="primary-button token-submit" disabled={busy || !id.trim() || (kind === 'agent' && !role.trim())}>{busy ? t('adminCreating') : t('adminCreate')}</button>
            </>}
        </form>
        {error && <div className="auth-error" role="alert">{t(error)}</div>}
        {issued && <section className="admin-result" aria-label={t('adminIssued')}>
          <h2>{t('adminIssued')}</h2><p>{issued.id} · {issued.kind}{issued.role && ` · ${issued.role}`}</p>
          <p>{t(issued.kind === 'human' ? 'adminHumanUse' : 'adminAgentUse')}</p><p>{t('adminOnce')}</p>
          <label htmlFor="issued-token">{t('identityToken')}</label><textarea id="issued-token" readOnly value={issued.token} spellCheck={false} />
          <div className="admin-actions"><button type="button" onClick={() => void copyToken()}>{t('adminCopy')}</button><button type="button" onClick={dismissResult}>{t('close')}</button></div>
          {copyStatus && <p role="status">{t(copyStatus)}</p>}
        </section>}
        {(authenticated || busy) && <button className="admin-link" type="button" onClick={clearSession}>{t('adminEnd')}</button>}
        <a className="admin-link" href="/" onClick={clearSession}>{t('adminBack')}</a>
      </section>
    </main>
  </div>
}
