import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { APIError } from './api'
import { changeIdentityToken, createIdentity, listIdentities } from './adminApi'
import { useI18n } from './i18n'
import type { CreateIdentityInput, IdentityRecord, IssuedIdentityToken } from './types'

export function AdminIdentitiesPage({ onLogout }: { onLogout: () => void }) {
  const { t } = useI18n()
  const request = useRef<AbortController | null>(null)
  const generation = useRef(0)
  const resultGeneration = useRef(0)
  const loading = useRef<AbortController | null>(null)
  const [identities, setIdentities] = useState<IdentityRecord[]>([])
  const [loaded, setLoaded] = useState(false)
  const [selection, setSelection] = useState<{ identity: IdentityRecord; action: 'rotate' | 'revoke' } | null>(null)
  const [busy, setBusy] = useState(false)
  const [id, setID] = useState('')
  const [kind, setKind] = useState<CreateIdentityInput['kind']>('human')
  const [role, setRole] = useState('')
  const [issued, setIssued] = useState<IssuedIdentityToken | null>(null)
  const [error, setError] = useState<'adminRejected' | 'adminUnavailable' | 'adminInvalid' | 'adminConflict' | 'adminUncertain' | 'adminForbidden' | null>(null)
  const [copyStatus, setCopyStatus] = useState<'adminCopied' | 'adminCopyFailed' | null>(null)

  const invalidateRequests = useCallback(() => {
    generation.current++
    resultGeneration.current++
    loading.current?.abort()
    request.current?.abort()
    request.current = null
  }, [])

  const clearSession = useCallback(() => {
    invalidateRequests()
    setIdentities([]); setLoaded(false); setSelection(null); setBusy(false)
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

  const refresh = useCallback(async (controller: AbortController) => {
    loading.current?.abort()
    loading.current = controller
    try {
      const records = await listIdentities(controller.signal)
      if (!controller.signal.aborted) { setIdentities(records); setLoaded(true) }
    } catch {
      if (!controller.signal.aborted) { setLoaded(false); setError('adminUnavailable') }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void refresh(controller)
    return () => controller.abort()
  }, [refresh])

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!id.trim() || (kind === 'agent' && !role.trim())) return
    await mutate(() => createIdentity({ id, kind, role: kind === 'agent' ? role.trim() : '' }, request.current!.signal))
  }

  async function mutate(operation: () => Promise<IssuedIdentityToken | null>) {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    const current = ++generation.current
    resultGeneration.current++
    setBusy(true); setError(null); setIssued(null); setCopyStatus(null)
    try {
      const result = await operation()
      if (current !== generation.current) return
      setIssued(result); setID(''); setRole(''); setSelection(null)
      await refresh(controller)
    } catch (cause) {
      if (current !== generation.current) return
      if (cause instanceof APIError && cause.status === 401) { clearSession(); setError('adminRejected') }
      else if (cause instanceof APIError && cause.status === 403) setError('adminForbidden')
      else if (cause instanceof APIError && cause.status === 409) setError('adminConflict')
      else if (cause instanceof APIError && cause.status === 400) setError('adminInvalid')
      else setError('adminUncertain')
    } finally {
      if (current === generation.current) { request.current = null; setBusy(false) }
    }
  }

  async function copyToken() {
    if (!issued) return
    const current = resultGeneration.current
    try {
      await navigator.clipboard.writeText(issued.token)
      if (current === resultGeneration.current) setCopyStatus('adminCopied')
    } catch {
      if (current === resultGeneration.current) setCopyStatus('adminCopyFailed')
    }
  }

  function dismissResult() {
    resultGeneration.current++
    setIssued(null); setCopyStatus(null)
  }

  return <section className="token-login admin-panel">
        <h1>{t('adminIdentities')}</h1>
        <p>{t('adminBody')}</p>
        <form className="admin-form" onSubmit={submit}>
              <label htmlFor="new-identity-id">{t('adminID')}</label><input id="new-identity-id" value={id} disabled={busy} autoComplete="off" onChange={event => setID(event.target.value)} />
              <label htmlFor="new-identity-kind">{t('adminKind')}</label><select id="new-identity-kind" value={kind} disabled={busy} onChange={event => { setKind(event.target.value as CreateIdentityInput['kind']); setRole('') }}><option value="human">Human</option><option value="agent">Agent</option></select>
              {kind === 'agent' && <><label htmlFor="new-identity-role">{t('adminRole')}</label><input id="new-identity-role" value={role} disabled={busy} placeholder="developer" onChange={event => setRole(event.target.value)} /></>}
              <button className="primary-button token-submit" disabled={busy || !loaded || !id.trim() || (kind === 'agent' && !role.trim())}>{busy ? t('adminCreating') : t('adminCreate')}</button>
        </form>
        {error && <div className="auth-error" role="alert">{t(error)}</div>}
        {issued && <section className="admin-result" aria-label={t('adminIssued')}>
          <h2>{t('adminIssued')}</h2><p>{issued.id} · {issued.kind}{issued.role && ` · ${issued.role}`}</p>
          <p>{t(issued.kind === 'human' ? 'adminHumanUse' : 'adminAgentUse')}</p><p>{t('adminOnce')}</p>
          <label htmlFor="issued-token">{t('identityToken')}</label><textarea id="issued-token" readOnly value={issued.token} spellCheck={false} />
          <div className="admin-actions"><button type="button" onClick={() => void copyToken()}>{t('adminCopy')}</button><button type="button" onClick={dismissResult}>{t('close')}</button></div>
          {copyStatus && <p role="status">{t(copyStatus)}</p>}
        </section>}
        <section className="admin-list" aria-label={t('adminExisting')}>
          <h2>{t('adminExisting')}</h2>
          <button type="button" disabled={busy} onClick={() => void mutate(async () => null)}>{t('adminRefresh')}</button>
          {!loaded ? <p>{t('adminLoading')}</p> : identities.length === 0 ? <p>{t('adminEmpty')}</p> : identities.map(record => <div className="admin-record" key={`${record.kind}:${record.id}`}>
            <strong>{record.id}</strong><p>{record.kind}{record.role && ` · ${record.role}`} · {t(record.credential_source === 'admin' ? 'adminManaged' : record.token_active ? 'adminActive' : 'adminRevoked')}</p>
            {record.credential_source !== 'admin' && <div className="admin-actions">
              <button type="button" disabled={busy} onClick={() => { dismissResult(); setSelection({ identity: record, action: 'rotate' }) }}>{t('adminRotate')}</button>
              <button type="button" disabled={busy || !record.token_active} onClick={() => { dismissResult(); setSelection({ identity: record, action: 'revoke' }) }}>{t('adminRevoke')}</button>
            </div>}
        {selection && selection.identity.id === record.id && selection.identity.kind === record.kind && <section className="admin-result" aria-label={t('adminConfirm')}>
          <h2>{t(selection.action === 'rotate' ? 'adminRotate' : 'adminRevoke')} · {selection.identity.id}</h2>
          <p>{t(selection.action === 'rotate' ? 'adminRotateWarning' : 'adminRevokeWarning')}</p>
          <div className="admin-actions"><button type="button" disabled={busy} onClick={() => void mutate(() => changeIdentityToken(selection.identity, selection.action, request.current!.signal))}>{t('adminConfirm')}</button><button type="button" disabled={busy} onClick={() => setSelection(null)}>{t('cancel')}</button></div>
        </section>}
          </div>)}
        </section>
        <button className="admin-link" type="button" onClick={() => { clearSession(); onLogout() }}>{t('logout')}</button>
      </section>
}
