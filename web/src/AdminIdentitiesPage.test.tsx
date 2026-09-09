// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AdminIdentitiesPage } from './AdminIdentitiesPage'
import { I18nProvider } from './i18n'
import { authenticationRequiredEvent } from './api'

const adminToken = 'synthetic-admin'
const issued = { id: 'new-human', kind: 'human', role: '', token: 'synthetic-issued' }
const response = (status = 200, data: unknown = []) => new Response(JSON.stringify({ data }), { status })
let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>
function renderPage() { return render(<I18nProvider><AdminIdentitiesPage /></I18nProvider>) }
async function connect() {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Admin Token'), adminToken)
  await user.click(screen.getByRole('button', { name: 'Verify Admin Token' }))
  await screen.findByLabelText('Identity ID')
  return user
}
beforeEach(() => {
  localStorage.setItem('kairos-console-locale', 'en')
  sessionStorage.setItem('kairos-console-token', 'synthetic-business')
  fetchMock = vi.fn<typeof fetch>().mockResolvedValue(response())
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); sessionStorage.clear() })

describe('administrator session', () => {
  it('uses explicit credentials, avoids storage and caches, and clears rejected input without expiring business login', async () => {
    fetchMock.mockResolvedValue(response(401))
    const expired = vi.fn()
    window.addEventListener(authenticationRequiredEvent, expired)
    renderPage()
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Admin Token'), adminToken)
    await user.click(screen.getByRole('button', { name: 'Verify Admin Token' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Admin Token rejected')
    expect(screen.getByLabelText('Admin Token')).toHaveValue('')
    expect(sessionStorage.getItem('kairos-console-token')).toBe('synthetic-business')
    expect(expired).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/identities', expect.objectContaining({ method: 'GET', headers: { Authorization: `Bearer ${adminToken}`, Accept: 'application/json' }, cache: 'no-store', redirect: 'error', credentials: 'omit' }))
    window.removeEventListener(authenticationRequiredEvent, expired)
  })

  it('creates and copies a Human token once; clears Agent role on type change and preserves meaningful ID whitespace', async () => {
    renderPage()
    const user = await connect()
    await user.selectOptions(screen.getByLabelText('Identity type'), 'agent')
    await user.type(screen.getByLabelText('Agent role'), 'developer')
    await user.selectOptions(screen.getByLabelText('Identity type'), 'human')
    expect(screen.queryByLabelText('Agent role')).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Identity ID'), ' 人 / ID ')
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    expect(await screen.findByLabelText('Identity Token')).toHaveValue(issued.token)
    expect(JSON.parse(fetchMock.mock.calls[1][1]!.body as string)).toEqual({ id: ' 人 / ID ', kind: 'human', role: '' })
    const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    await user.click(screen.getByRole('button', { name: 'Copy Token' }))
    expect(clipboard).toHaveBeenCalledWith(issued.token)
    expect(await screen.findByRole('status')).toHaveTextContent('Token copied')
    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    expect(sessionStorage.length).toBe(1)
    expect(localStorage.length).toBe(1)
  })

  it('requires nonblank ID and Agent role, trims role, and prevents duplicate in-flight creation', async () => {
    renderPage()
    const user = await connect()
    const create = screen.getByRole('button', { name: 'Create identity and issue Token' })
    expect(create).toBeDisabled()
    await user.type(screen.getByLabelText('Identity ID'), 'new-agent')
    await user.selectOptions(screen.getByLabelText('Identity type'), 'agent')
    await user.type(screen.getByLabelText('Agent role'), '   ')
    expect(create).toBeDisabled()
    await user.type(screen.getByLabelText('Agent role'), 'developer ')
    fetchMock.mockImplementationOnce(() => new Promise(() => {}))
    await user.dblClick(create)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1]!.body as string)).toEqual({ id: 'new-agent', kind: 'agent', role: 'developer' })
    expect(screen.getByLabelText('Identity ID')).toBeDisabled()
  })

  it.each([400, 409, 500])('reports creation error %s without automatic retry', async status => {
    renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    fetchMock.mockResolvedValueOnce(response(status))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(status === 400 ? 'Invalid identity' : status === 409 ? 'already exists' : 'may already exist')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(screen.getByLabelText('Identity ID')).toHaveValue('new-human')
  })

  it('reports connection failure and permits manual copy when clipboard fails', async () => {
    renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    fetchMock.mockRejectedValueOnce(new TypeError('network'))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('No automatic retry')
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    await screen.findByLabelText('Identity Token')
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))
    await user.click(screen.getByRole('button', { name: 'Copy Token' }))
    expect(await screen.findByRole('status')).toHaveTextContent('copy it manually')
    await user.click(screen.getByRole('button', { name: 'End administrator session' }))
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Admin Token')).toHaveValue('')
  })

  it.each(['logout', 'pagehide', 'unmount'])('discards late creation responses after %s', async action => {
    const page = renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    let resolve!: (response: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    const signal = fetchMock.mock.calls[1][1]!.signal!
    if (action === 'logout') await user.click(screen.getByRole('button', { name: 'End administrator session' }))
    else if (action === 'pagehide') fireEvent(window, new Event('pagehide'))
    else page.unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => { resolve(response(201, issued)) })
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    if (action !== 'unmount') expect(screen.getByLabelText('Admin Token')).toHaveValue('')
  })

  it('clears the previous result when starting another creation and expires only admin on 401', async () => {
    renderPage()
    const user = await connect()
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    await screen.findByLabelText('Identity Token')
    await user.type(screen.getByLabelText('Identity ID'), 'second-human')
    fetchMock.mockResolvedValueOnce(response(401))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    await waitFor(() => expect(screen.getByLabelText('Admin Token')).toHaveValue(''))
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    expect(sessionStorage.getItem('kairos-console-token')).toBe('synthetic-business')
  })
})
