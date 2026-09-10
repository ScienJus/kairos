import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { Copy, Plus, RefreshCw } from 'lucide-react'
import { Modal } from './ui'
import { APIError } from './api'
import { changeIdentityToken, createIdentity, listIdentities } from './adminApi'
import { useI18n } from './i18n'
import type { CreateIdentityInput, IdentityRecord, IssuedIdentityToken } from './types'

export function AdminIdentitiesPage() {
  const { t } = useI18n()
  const request = useRef<AbortController | null>(null)
  const generation = useRef(0)
  const resultGeneration = useRef(0)
  const loading = useRef<AbortController | null>(null)
  const [identities, setIdentities] = useState<IdentityRecord[]>([])
  const [loaded, setLoaded] = useState(false)
  const [selection, setSelection] = useState<{ identity: IdentityRecord; action: 'rotate' | 'revoke' } | null>(null)
  const [creating, setCreating] = useState(false)
  const [copiedID, setCopiedID] = useState<string | null>(null)
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
    setCreating(false); setCopiedID(null); setID(''); setKind('human'); setRole(''); setIssued(null); setCopyStatus(null); setError(null)
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
    if (!idIsValid(id) || (kind === 'agent' && !role.trim())) return
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
      setCreating(false); setIssued(result); setID(''); setRole(''); setSelection(null)
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

  async function copyID(record: IdentityRecord) {
    dismissResult()
    const current = generation.current
    try {
      await navigator.clipboard.writeText(record.id)
      if (current === generation.current) setCopiedID(`${record.kind}:${record.id}`)
    } catch {
      if (current === generation.current) setCopiedID('failed')
    }
  }

  function openCreate() {
    dismissResult(); setSelection(null); setError(null); setCreating(true)
  }

  function closeDialog() {
    if (busy) return
    setCreating(false); setSelection(null); setID(''); setRole(''); setKind('human'); setError(null)
  }

  const errorMessage = error && <div className="form-error" role="alert">{t(error)}</div>
  return <section className="blackboards-page identities-page">
    <header className="library-heading">
      <div><span>{t('adminExisting')}</span><h1>{t('adminIdentities')}</h1><p>{t('adminBody')}</p></div>
      <div className="identity-actions">
        <button className="quiet-button" type="button" disabled={busy} onClick={() => void mutate(async () => null)}><RefreshCw size={15} />{t(busy ? 'adminWorking' : 'adminRefresh')}</button>
        <button className="primary-button" type="button" disabled={busy || !loaded} onClick={openCreate}><Plus size={16} />{t('adminNew')}</button>
      </div>
    </header>
    {!creating && !selection && errorMessage}
    {issued && <section className="identity-result" aria-label={t('adminIssued')}>
      <h2>{t('adminIssued')}</h2><p>{issued.id} · {issued.kind}{issued.role && ` · ${issued.role}`}</p>
      <p>{t(issued.kind === 'human' ? 'adminHumanUse' : 'adminAgentUse')}</p><p className="identity-once">{t('adminOnce')}</p>
      <label htmlFor="issued-token">{t('identityToken')}</label><textarea id="issued-token" readOnly value={issued.token} spellCheck={false} />
      <div className="identity-actions"><button className="primary-button" type="button" onClick={() => void copyToken()}><Copy size={15} />{t('adminCopy')}</button><button className="quiet-button" type="button" onClick={dismissResult}>{t('close')}</button></div>
      {copyStatus && <p role="status">{t(copyStatus)}</p>}
    </section>}
    <section aria-label={t('adminExisting')}>
      {!loaded ? <p className="library-empty">{error ? t('adminUnavailable') : t('adminLoading')}</p> : identities.length === 0 ? <p className="library-empty">{t('adminEmpty')}</p> : <table className="identity-table">
        <thead><tr><th>{t('adminIdentity')}</th><th>{t('adminTypeRole')}</th><th>{t('adminStatus')}</th><th>{t('adminActions')}</th></tr></thead>
        <tbody>{identities.map(record => <tr key={`${record.kind}:${record.id}`}>
          <td data-label={t('adminIdentity')}><div className="identity-name">{record.credential_source === 'admin' ? 'system admin' : record.id}</div>
            <div className="identity-id"><code>{record.id}</code><button className="icon-button" type="button" aria-label={`${t('adminCopyID')}: ${record.id}`} onClick={() => void copyID(record)}><Copy size={14} /></button></div>
            {copiedID === `${record.kind}:${record.id}` && <small role="status">{t('adminIDCopied')}</small>}
          </td>
          <td data-label={t('adminTypeRole')}><span>{record.kind === 'human' ? 'Human' : 'Agent'}</span>{record.role && <small className="identity-role">{record.role}</small>}</td>
          <td data-label={t('adminStatus')}><span className={`identity-status ${record.credential_source === 'admin' ? 'managed' : record.token_active ? 'active' : 'inactive'}`}>{t(record.credential_source === 'admin' ? 'adminManaged' : record.token_active ? 'adminActive' : 'adminRevoked')}</span></td>
          <td data-label={t('adminActions')}>{record.credential_source !== 'admin' ? <div className="identity-actions">
            <button className="quiet-button" type="button" disabled={busy} onClick={() => { dismissResult(); setError(null); setSelection({ identity: record, action: 'rotate' }) }}>{t('adminRotate')}</button>
            <button className="quiet-button danger-button" type="button" disabled={busy || !record.token_active} onClick={() => { dismissResult(); setError(null); setSelection({ identity: record, action: 'revoke' }) }}>{t('adminRevoke')}</button>
          </div> : <span className="identity-readonly">{t('adminReadOnly')}</span>}</td>
        </tr>)}</tbody>
      </table>}
      {copiedID === 'failed' && <p role="status">{t('adminIDCopyFailed')}</p>}
    </section>
    <Modal open={creating || selection !== null} onOpenChange={open => { if (!open) closeDialog() }} title={t(creating ? 'adminNew' : selection?.action === 'revoke' ? 'adminRevoke' : 'adminRotate')} eyebrow={t('adminIdentities')}>
      {creating ? <form className="form-grid" onSubmit={submit}>
        <label className="wide" htmlFor="new-identity-id">{t('adminID')}<input id="new-identity-id" value={id} disabled={busy} autoComplete="off" aria-describedby="identity-id-hint" onChange={event => setID(event.target.value)} /></label><p className="wide identity-field-hint" id="identity-id-hint">{t('adminIDHint')}</p>
        <label htmlFor="new-identity-kind">{t('adminKind')}<select id="new-identity-kind" value={kind} disabled={busy} onChange={event => { setKind(event.target.value as CreateIdentityInput['kind']); setRole('') }}><option value="human">Human</option><option value="agent">Agent</option></select></label>
        {kind === 'agent' && <label htmlFor="new-identity-role">{t('adminRole')}<input id="new-identity-role" value={role} disabled={busy} placeholder="developer" onChange={event => setRole(event.target.value)} /></label>}
        {errorMessage}
        <div className="form-actions"><button type="button" className="quiet-button" disabled={busy} onClick={closeDialog}>{t('cancel')}</button><button className="primary-button" disabled={busy || !loaded || !idIsValid(id) || (kind === 'agent' && !role.trim())}>{t(busy ? 'adminWorking' : 'adminCreate')}</button></div>
      </form> : selection && <div className="identity-confirm">
        <p className="identity-confirm-id">{selection.identity.id}</p><p>{t(selection.action === 'rotate' ? 'adminRotateWarning' : 'adminRevokeWarning')}</p>
        {errorMessage}
        <div className="identity-actions"><button className="quiet-button" type="button" disabled={busy} onClick={closeDialog}>{t('cancel')}</button><button className={selection.action === 'revoke' ? 'quiet-button danger-button' : 'primary-button'} type="button" disabled={busy} onClick={() => void mutate(() => changeIdentityToken(selection.identity, selection.action, request.current!.signal))}>{t(busy ? 'adminWorking' : 'adminConfirm')}</button></div>
      </div>}
    </Modal>
  </section>
}

// Matches ActorRef.Validate; preserve meaningful whitespace and Unicode IDs.
function idIsValid(id: string) { return Boolean(id.trim()) && id !== '.' && id !== '..' }
