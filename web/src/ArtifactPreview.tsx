import { useEffect, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { APIError, api } from './api'
import { ArtifactPreviewError, artifactPreviewMaxBytes } from './artifactText'
import { useI18n } from './i18n'
import type { Artifact, Identity } from './types'
import { FormError, Modal } from './ui'
import './styles/artifact-preview.css'

export default function ArtifactPreview({ artifact, identity, onClose, onDownload, downloadError }: {
  artifact: Artifact; identity: Identity; onClose: () => void; onDownload: () => void; downloadError: Error | null
}) {
  const { t } = useI18n()
  const [state, setState] = useState<{ text?: string; error?: Error }>({})
  const [source, setSource] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const { id: actorID, kind, role } = identity
  const artifactID = artifact.id

  useEffect(() => {
    const controller = new AbortController()
    api.previewArtifact({ id: actorID, kind, role }, artifactID, controller.signal).then(text => {
      if (!controller.signal.aborted) setState({ text })
    }).catch(error => {
      if (!controller.signal.aborted) setState({ error: error instanceof Error ? error : new Error(String(error)) })
    })
    return () => controller.abort()
  }, [actorID, kind, role, artifactID, attempt])

  const error = state.error
  const message = error instanceof ArtifactPreviewError
    ? t(error.reason === 'too_large' ? 'artifactPreviewTooLarge' : 'artifactPreviewNotText', { limit: artifactPreviewMaxBytes / 1024 })
    : error instanceof APIError && error.status === 403 ? t('artifactPreviewForbidden') : error?.message

  return <Modal open onOpenChange={open => { if (!open) onClose() }} onEscapeKeyDown={event => event.stopPropagation()} title={artifact.name} eyebrow={t('artifactPreview')} className="artifact-preview-dialog">
    <div className="artifact-preview-body">
      {state.text === undefined && !error && <p className="artifact-preview-notice" role="status">{t('artifactPreviewLoading')}</p>}
      {message && <><FormError error={new Error(message)} />{!(error instanceof ArtifactPreviewError) && <button type="button" className="quiet-button" onClick={() => { setState({}); setAttempt(value => value + 1) }}>{t('artifactPreviewRetry')}</button>}</>}
      {state.text !== undefined && <>
        <div className="artifact-preview-views" role="group" aria-label={t('artifactPreviewViews')}>
          <button type="button" aria-pressed={!source} onClick={() => setSource(false)}>Markdown</button>
          <button type="button" aria-pressed={source} onClick={() => setSource(true)}>{t('artifactPreviewSource')}</button>
        </div>
        {state.text === '' ? <p className="artifact-preview-notice">{t('artifactPreviewEmpty')}</p> : source
          ? <pre className="artifact-preview-source"><code>{state.text}</code></pre>
          : <div className="artifact-markdown"><Markdown remarkPlugins={[remarkGfm]} skipHtml disallowedElements={['img']}
            urlTransform={url => /^https?:\/\//i.test(url) ? url : ''}
            components={{ a: ({ href, children }) => href ? <a href={href} target="_blank" rel="noreferrer">{children}</a> : <span>{children}</span> }}
          >{state.text}</Markdown></div>}
      </>}
      {downloadError && <FormError error={downloadError} />}
    </div>
    <footer className="artifact-preview-footer"><span>{t('artifactPreviewLimit', { limit: artifactPreviewMaxBytes / 1024 })}</span><button type="button" className="quiet-button" onClick={onDownload}>{t('downloadArtifact')}</button></footer>
  </Modal>
}
