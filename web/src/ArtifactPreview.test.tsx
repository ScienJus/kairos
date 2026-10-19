// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest'
import { act, cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { APIError, api } from './api'
import { ArtifactPreviewError } from './artifactText'
import { I18nProvider } from './i18n'
import { ArtifactLinks } from './TaskArtifacts'
import type { Artifact, Identity } from './types'

const identity: Identity = { id: 'reviewer', kind: 'human', role: '' }
const artifact: Artifact = { id: 'draft', name: 'draft', uri: 'kairos://blobs/draft', work_item_id: 'work', task_id: 'writer', claim_id: 'claim', submission_id: 'submission', created_at: '2026-10-19T03:00:00Z' }

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

it('loads only on demand and switches between rendered Markdown and exact source', async () => {
  const text = '# Delivery\n\n- Reviewed\n\n```go\nfmt.Println("done")\n```\n\n| Result | Status |\n| --- | --- |\n| Tests | Passed |'
  const preview = vi.spyOn(api, 'previewArtifact').mockResolvedValue(text)
  render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  expect(preview).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
  const dialog = await screen.findByRole('dialog', { name: 'draft' })
  expect(await within(dialog).findByRole('heading', { name: 'Delivery' })).toBeInTheDocument()
  expect(within(dialog).getByRole('cell', { name: 'Passed' })).toBeInTheDocument()
  expect(dialog.querySelector('pre code')).toHaveTextContent('fmt.Println("done")')
  expect(preview).toHaveBeenCalledWith(identity, artifact.id, expect.any(AbortSignal))
  await userEvent.click(within(dialog).getByRole('button', { name: 'Source' }))
  expect(dialog.querySelector('pre code')?.textContent).toBe(text)
  expect(within(dialog).getByRole('button', { name: 'Download' })).toBeInTheDocument()
})

it('discards raw HTML, remote images, relative links and unsafe URL schemes', async () => {
  vi.spyOn(api, 'previewArtifact').mockResolvedValue('<script>alert("bad")</script>\n\n<img src="https://tracker.invalid/pixel">\n\n![tracking](https://tracker.invalid/image)\n\n[unsafe](javascript:alert%281%29) [relative](/admin/identities) [Review](https://example.com/pr)')
  render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
  const dialog = await screen.findByRole('dialog', { name: 'draft' })
  const link = await within(dialog).findByRole('link', { name: 'Review' })
  expect(link).toHaveAttribute('href', 'https://example.com/pr')
  expect(link).toHaveAttribute('rel', 'noreferrer')
  expect(dialog.querySelectorAll('script, img, iframe')).toHaveLength(0)
  expect(within(dialog).queryByRole('link', { name: 'unsafe' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('link', { name: 'relative' })).not.toBeInTheDocument()
})

it('aborts on Escape, preserves the containing Task panel and ignores late content', async () => {
  let resolveFirst!: (text: string) => void
  const preview = vi.spyOn(api, 'previewArtifact').mockReturnValueOnce(new Promise(resolve => { resolveFirst = resolve })).mockResolvedValue('Second artifact')
  const escaped = vi.fn()
  window.addEventListener('keydown', escaped)
  try {
    render(<I18nProvider><ArtifactLinks artifacts={[artifact, { ...artifact, id: 'second', name: 'second' }]} identity={identity} /></I18nProvider>)
    await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
    await screen.findByRole('dialog', { name: 'draft' })
    const signal = preview.mock.calls[0][2]
    await userEvent.keyboard('{Escape}')
    expect(signal.aborted).toBe(true)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(escaped).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Preview: second' }))
    expect(await screen.findByText('Second artifact')).toBeInTheDocument()
    await act(async () => resolveFirst('Stale confidential content'))
    expect(screen.queryByText('Stale confidential content')).not.toBeInTheDocument()
  } finally { window.removeEventListener('keydown', escaped) }
})

it.each([
  { error: new ArtifactPreviewError('too_large'), message: /exceeds the 256 KiB preview limit/ },
  { error: new ArtifactPreviewError('not_text'), message: /cannot be previewed as text/ },
  { error: new APIError(403, 'Request failed (403)'), message: /do not have permission/ },
])('keeps download available when preview fails ($error.message)', async ({ error, message }) => {
  vi.spyOn(api, 'previewArtifact').mockRejectedValue(error)
  vi.spyOn(api, 'downloadArtifact').mockRejectedValue(new Error('Download unavailable'))
  render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
  const dialog = await screen.findByRole('dialog', { name: 'draft' })
  expect(await within(dialog).findByText(message)).toBeInTheDocument()
  await userEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(await within(dialog).findByText('Download unavailable')).toBeInTheDocument()
})

it('retries a failed read and distinguishes an empty artifact', async () => {
  vi.spyOn(api, 'previewArtifact').mockRejectedValueOnce(new Error('Connection interrupted')).mockResolvedValue('')
  render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
  const dialog = await screen.findByRole('dialog', { name: 'draft' })
  expect(await within(dialog).findByText('Connection interrupted')).toBeInTheDocument()
  await userEvent.click(within(dialog).getByRole('button', { name: 'Try again' }))
  expect(await within(dialog).findByText('This artifact is empty.')).toBeInTheDocument()
})

it('does not show a late download failure from another artifact in the preview', async () => {
  let rejectDownload!: (error: Error) => void
  vi.spyOn(api, 'downloadArtifact').mockReturnValue(new Promise((_, reject) => { rejectDownload = reject }))
  vi.spyOn(api, 'previewArtifact').mockResolvedValue('Second artifact')
  render(<I18nProvider><ArtifactLinks artifacts={[artifact, { ...artifact, id: 'second', name: 'second' }]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getAllByRole('button', { name: 'Download' })[0])
  await userEvent.click(screen.getByRole('button', { name: 'Preview: second' }))
  const dialog = await screen.findByRole('dialog', { name: 'second' })
  expect(await within(dialog).findByText('Second artifact')).toBeInTheDocument()
  await act(async () => rejectDownload(new Error('First artifact download failed')))
  expect(within(dialog).queryByText('First artifact download failed')).not.toBeInTheDocument()
})

it('keeps external artifacts as links without offering a managed preview', () => {
  render(<I18nProvider><ArtifactLinks artifacts={[{ ...artifact, uri: 'https://example.com/report.md' }]} identity={identity} /></I18nProvider>)
  expect(screen.getByRole('link')).toHaveAttribute('href', 'https://example.com/report.md')
  expect(screen.queryByRole('button', { name: /Preview/ })).not.toBeInTheDocument()
})

it('clears loaded text when the viewing identity changes and rechecks access', async () => {
  vi.spyOn(api, 'previewArtifact').mockResolvedValueOnce('First actor content').mockRejectedValueOnce(new APIError(403, 'Forbidden'))
  const view = render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getByRole('button', { name: 'Preview: draft' }))
  expect(await screen.findByText('First actor content')).toBeInTheDocument()
  view.rerender(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={{ ...identity, id: 'other-viewer' }} /></I18nProvider>)
  expect(screen.queryByText('First actor content')).not.toBeInTheDocument()
  expect(await screen.findByText('You do not have permission to read this artifact.')).toBeInTheDocument()
})

it('shows Chinese preview controls and size feedback', async () => {
  localStorage.setItem('kairos-console-locale', 'zh-CN')
  vi.spyOn(api, 'previewArtifact').mockRejectedValue(new ArtifactPreviewError('too_large'))
  render(<I18nProvider><ArtifactLinks artifacts={[artifact]} identity={identity} /></I18nProvider>)
  await userEvent.click(screen.getByRole('button', { name: '预览: draft' }))
  const dialog = await screen.findByRole('dialog', { name: 'draft' })
  expect(await within(dialog).findByText('文件超过 256 KiB 的预览上限，请下载查看。')).toBeInTheDocument()
  expect(within(dialog).getByRole('button', { name: '下载' })).toBeInTheDocument()
})
