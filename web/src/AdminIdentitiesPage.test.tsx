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
  await screen.findByText('No identities yet.')
  await user.click(screen.getByRole('button', { name: 'Create identity' }))
  return user
}
beforeEach(() => {
  localStorage.setItem('kairos-console-locale', 'en')
  sessionStorage.setItem('kairos-console-token', adminToken)
  fetchMock = vi.fn<typeof fetch>().mockImplementation(async () => response())
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); sessionStorage.clear() })

describe('administrator session', () => {
  it('uses the login credential and expires the current session on rejection', async () => {
    fetchMock.mockResolvedValue(response(401))
    const expired = vi.fn()
    window.addEventListener(authenticationRequiredEvent, expired)
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('Unable to load identities')
    expect(expired).toHaveBeenCalledOnce()
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
    expect(await screen.findByRole('alert')).toHaveTextContent(status === 400 ? 'Invalid identity' : status === 409 ? 'already exists' : 'may already have changed')
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
    fireEvent(window, new Event('pagehide'))
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  })

  it.each(['pagehide', 'unmount'])('discards late creation responses after %s', async action => {
    const page = renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    let resolve!: (response: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    const signal = fetchMock.mock.calls[1][1]!.signal!
    if (action === 'pagehide') fireEvent(window, new Event('pagehide'))
    else page.unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => { resolve(response(201, issued)) })
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    if (action !== 'unmount') expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  })

  it('clears the previous result when starting another creation and clears results on 401', async () => {
    renderPage()
    const user = await connect()
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    await screen.findByLabelText('Identity Token')
    await user.click(screen.getByRole('button', { name: 'Create identity' }))
    await user.type(screen.getByLabelText('Identity ID'), 'second-human')
    fetchMock.mockResolvedValueOnce(response(401))
    await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
    await waitFor(() => expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument())
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
    expect(sessionStorage.getItem('kairos-console-token')).toBe(adminToken)
  })
})

it('confirms rotation and revocation, handles 204, and keeps the deployment credential read-only', async () => {
  const records = [
    { id: 'deployment', kind: 'human', role: '', credential_source: 'admin', token_active: false },
    { id: 'person / one', kind: 'human', role: '', credential_source: 'identity', token_active: true },
  ]
  fetchMock.mockImplementation(async () => response(200, records))
  renderPage()
  const user = userEvent.setup()
  await screen.findByText('system admin')
  expect(screen.getByRole('table')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Identity ID')).not.toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: 'Rotate Token' })).toHaveLength(1)
  await user.click(screen.getByRole('button', { name: 'Rotate Token' }))
  expect(fetchMock).toHaveBeenCalledTimes(1)
  expect(screen.getByText(/current Token will stop working immediately/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(fetchMock).toHaveBeenCalledTimes(1)
  await user.click(screen.getByRole('button', { name: 'Rotate Token' }))
  fetchMock.mockResolvedValueOnce(response(200, { ...issued, id: records[1].id }))
  await user.click(screen.getByRole('button', { name: 'Confirm change' }))
  expect(await screen.findByLabelText('Identity Token')).toHaveValue(issued.token)
  expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/identities/human/person%20%2F%20one/token')
  expect(fetchMock.mock.calls[1][1]!.method).toBe('POST')
  await user.click(screen.getByRole('button', { name: 'Revoke Token' }))
  expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
  records[1].token_active = false
  await user.click(screen.getByRole('button', { name: 'Confirm change' }))
  expect(await screen.findByText(/Token inactive/)).toBeInTheDocument()
  expect(fetchMock.mock.calls[3][1]!.method).toBe('DELETE')
  expect(screen.getByRole('button', { name: 'Revoke Token' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Rotate Token' })).toBeEnabled()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

it('does not let delayed copy feedback or list responses restore dismissed state', async () => {
  const page = renderPage()
  const user = await connect()
  fetchMock.mockResolvedValueOnce(response(201, issued))
  await user.type(screen.getByLabelText('Identity ID'), 'new-human')
  await user.click(screen.getByRole('button', { name: 'Create identity and issue Token' }))
  await screen.findByLabelText('Identity Token')
  let done!: () => void
  vi.spyOn(navigator.clipboard, 'writeText').mockImplementation(() => new Promise(resolve => { done = resolve }))
  await user.click(screen.getByRole('button', { name: 'Copy Token' }))
  await user.click(screen.getByRole('button', { name: 'Close' }))
  await act(async () => done())
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
  page.unmount()
  let listDone!: (value: Response) => void
  fetchMock.mockImplementationOnce(() => new Promise(resolve => { listDone = resolve }))
  renderPage()
  const signal = fetchMock.mock.calls.at(-1)![1]!.signal!
  fireEvent(window, new Event('pagehide'))
  expect(signal.aborted).toBe(true)
  await act(async () => listDone(response(200, [{ id: 'late', kind: 'human' }])))
  expect(screen.queryByText('late')).not.toBeInTheDocument()
})

 it('keeps creation in a dismissible dialog and rejects reserved path IDs before sending', async () => {
  renderPage()
  const user = await connect()
  expect(screen.getByRole('dialog', { name: 'Create identity' })).toBeInTheDocument()
  for (const id of ['.', '..', '   ']) {
    await user.clear(screen.getByLabelText('Identity ID'))
    await user.type(screen.getByLabelText('Identity ID'), id)
    expect(screen.getByRole('button', { name: 'Create identity and issue Token' })).toBeDisabled()
    fireEvent.submit(screen.getByLabelText('Identity ID').closest('form')!)
  }
  expect(fetchMock).toHaveBeenCalledTimes(1)
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Create identity' }))
  expect(screen.getByLabelText('Identity ID')).toHaveValue('')
 })

 it('copies the full deployment ID while presenting system admin and no credential actions', async () => {
  const id = 'admin-long-internal-identity'
  fetchMock.mockResolvedValue(response(200, [{ id, kind: 'human', role: '', credential_source: 'admin', token_active: false }]))
  renderPage()
  const user = userEvent.setup()
  expect(await screen.findByText('system admin')).toBeInTheDocument()
  const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  await user.click(screen.getByRole('button', { name: `Copy ID: ${id}` }))
  expect(clipboard).toHaveBeenCalledWith(id)
  expect(screen.getByRole('status')).toHaveTextContent('ID copied')
  expect(screen.queryByRole('button', { name: 'Rotate Token' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Revoke Token' })).not.toBeInTheDocument()
 })
