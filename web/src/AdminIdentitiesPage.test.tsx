// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  Element.prototype.scrollIntoView = vi.fn()
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
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
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
    const create = screen.getByRole('button', { name: 'Create and issue Token' })
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
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(status === 400 ? 'Invalid identity' : status === 409 ? 'already exists' : 'may already have changed')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(screen.getByLabelText('Identity ID')).toHaveValue('new-human')
  })

  it('reports connection failure and permits manual copy when clipboard fails', async () => {
    renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    fetchMock.mockRejectedValueOnce(new TypeError('network'))
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('No automatic retry')
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    await screen.findByLabelText('Identity Token')
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))
    await user.click(screen.getByRole('button', { name: 'Copy Token' }))
    expect(await screen.findByRole('status')).toHaveTextContent('copy it manually')
    fireEvent(window, new Event('pagehide'))
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  })

  it.each(['pagehide', 'unmount'])('discards late creation responses after %s', async action => {
    const page = renderPage()
    const user = await connect()
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    let resolve!: (response: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    const signal = fetchMock.mock.calls[1][1]!.signal!
    if (action === 'pagehide') fireEvent(window, new Event('pagehide'))
    else page.unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => { resolve(response(201, issued)) })
    expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  })

  it('clears the previous result when starting another creation and clears results on 401', async () => {
    renderPage()
    const user = await connect()
    fetchMock.mockResolvedValueOnce(response(201, issued))
    await user.type(screen.getByLabelText('Identity ID'), 'new-human')
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    await screen.findByLabelText('Identity Token')
    await user.click(screen.getByRole('button', { name: 'Create identity' }))
    await user.type(screen.getByLabelText('Identity ID'), 'second-human')
    fetchMock.mockResolvedValueOnce(response(401))
    await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
    await waitFor(() => expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument())
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
  await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
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
    expect(screen.getByRole('button', { name: 'Create and issue Token' })).toBeDisabled()
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

it('shows ordinary IDs once and copies the full value', async () => {
  const records = [
    { id: 'local-human', kind: 'human', credential_source: 'identity', token_active: true },
    { id: 'named-human-with-a-long-internal-id', kind: 'human', credential_source: 'identity', token_active: true },
  ]
  fetchMock.mockResolvedValue(response(200, records))
  renderPage()
  const user = userEvent.setup()
  expect(await screen.findAllByText('local-human')).toHaveLength(1)
  expect(screen.getAllByText(records[1].id)).toHaveLength(1)
  expect(screen.getByText(records[1].id).closest('[title]')).toHaveAttribute('title', records[1].id)
  const copy = screen.getByRole('button', { name: `Copy ID: ${records[1].id}` })
  const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  copy.focus()
  await user.keyboard('{Enter}')
  expect(clipboard).toHaveBeenCalledWith(records[1].id)
  expect(copy).toHaveAttribute('title', 'ID copied.')
  expect(copy.querySelector('.lucide-check')).toBeInTheDocument()
  expect(copy).toHaveFocus()
})

it('keeps the current list and refresh label through loading and a failed refresh', async () => {
  fetchMock.mockResolvedValueOnce(response(200, [{ id: 'retained-human', kind: 'human', credential_source: 'identity', token_active: true }]))
  renderPage()
  const user = userEvent.setup()
  await screen.findByText('retained-human')
  let done!: (response: Response) => void
  fetchMock.mockImplementationOnce(() => new Promise(resolve => { done = resolve }))
  const refresh = screen.getByRole('button', { name: 'Refresh' })
  await user.click(refresh)
  expect(refresh).toBeDisabled()
  expect(refresh).toHaveAttribute('aria-busy', 'true')
  expect(screen.getByText('retained-human')).toBeInTheDocument()
  await act(async () => done(response(500)))
  expect(await screen.findByRole('alert')).toHaveTextContent('Unable to load identities')
  expect(screen.getByText('retained-human')).toBeInTheDocument()
  expect(refresh).toBeEnabled()
  expect(refresh).toHaveAttribute('aria-busy', 'false')
})

it('shows an example normally and a field-specific explanation for reserved IDs', async () => {
  renderPage()
  const user = await connect()
  const input = screen.getByLabelText('Identity ID')
  expect(input).toHaveAccessibleDescription('For example, alice or backend-agent.')
  await user.type(input, '..')
  expect(input).toHaveAttribute('aria-invalid', 'true')
  expect(input).toHaveAccessibleDescription('Enter an ID that is not blank, . or ..')
  await user.clear(input)
  await user.type(input, 'alice')
  expect(input).toHaveAttribute('aria-invalid', 'false')
  expect(input).toHaveAccessibleDescription('For example, alice or backend-agent.')
})

it.each(['create', 'rotate'])('preserves the %s result and Token copy feedback when copying an ID succeeds or fails', async operation => {
  const record = { id: issued.id, kind: 'human', role: '', credential_source: 'identity', token_active: true }
  fetchMock.mockImplementation(async () => response(200, [record]))
  renderPage()
  const user = userEvent.setup()
  await screen.findByRole('table')
  if (operation === 'create') {
    await user.click(screen.getByRole('button', { name: 'Create identity' }))
    await user.type(screen.getByLabelText('Identity ID'), issued.id)
  } else {
    await user.click(screen.getByRole('button', { name: 'Rotate Token' }))
  }
  fetchMock.mockResolvedValueOnce(response(operation === 'create' ? 201 : 200, issued))
  await user.click(screen.getByRole('button', { name: operation === 'create' ? 'Create and issue Token' : 'Confirm change' }))
  await screen.findByLabelText('Identity Token')
  const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  await user.click(screen.getByRole('button', { name: 'Copy Token' }))
  const result = screen.getByLabelText('Identity Token').closest('section')!
  expect(within(result).getByRole('status')).toHaveTextContent('Token copied')
  await user.click(screen.getByRole('button', { name: `Copy ID: ${issued.id}` }))
  expect(clipboard).toHaveBeenLastCalledWith(issued.id)
  expect(screen.getByLabelText('Identity Token')).toHaveValue(issued.token)
  expect(within(result).getByRole('status')).toHaveTextContent('Token copied')
  clipboard.mockRejectedValueOnce(new Error('denied'))
  await user.click(screen.getByRole('button', { name: `Copy ID: ${issued.id}` }))
  expect(screen.getByLabelText('Identity Token')).toHaveValue(issued.token)
  expect(screen.getByText(/Copy failed. Select the ID/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close' }))
  expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
})

it('reloads metadata after a persisted pageshow without restoring issued Tokens or replaying creation', async () => {
  const page = renderPage()
  const user = await connect()
  await user.type(screen.getByLabelText('Identity ID'), issued.id)
  fetchMock.mockResolvedValueOnce(response(201, issued))
  await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
  await screen.findByLabelText('Identity Token')
  await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled())
  const before = fetchMock.mock.calls.length
  fireEvent(window, new PageTransitionEvent('pageshow', { persisted: false }))
  expect(fetchMock).toHaveBeenCalledTimes(before)
  fireEvent(window, new PageTransitionEvent('pagehide', { persisted: true }))
  expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  fetchMock.mockResolvedValueOnce(response(200, [{ id: 'restored-human', kind: 'human' }]))
  fireEvent(window, new PageTransitionEvent('pageshow', { persisted: true }))
  expect(await screen.findByText('restored-human')).toBeInTheDocument()
  expect(screen.queryByLabelText('Identity Token')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Create identity' })).toBeEnabled()
  expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  const after = fetchMock.mock.calls.length
  page.unmount()
  fireEvent(window, new PageTransitionEvent('pageshow', { persisted: true }))
  expect(fetchMock).toHaveBeenCalledTimes(after)
})

it('keeps pre-navigation responses cancelled and permits retry after a failed cache restoration', async () => {
  let stale!: (value: Response) => void
  fetchMock.mockImplementationOnce(() => new Promise(resolve => { stale = resolve }))
  renderPage()
  const signal = fetchMock.mock.calls[0][1]!.signal!
  fireEvent(window, new PageTransitionEvent('pagehide', { persisted: true }))
  expect(signal.aborted).toBe(true)
  fetchMock.mockResolvedValueOnce(response(500))
  fireEvent(window, new PageTransitionEvent('pageshow', { persisted: true }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Unable to load identities')
  await act(async () => stale(response(200, [{ id: 'stale-human', kind: 'human' }])))
  expect(screen.queryByText('stale-human')).not.toBeInTheDocument()
  fetchMock.mockResolvedValueOnce(response(200, [{ id: 'current-human', kind: 'human' }]))
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(await screen.findByText('current-human')).toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})


it.each(['create', 'rotate'])('brings the %s result above an 80-row list and focuses it after closing the dialog', async operation => {
  const records = Array.from({ length: 80 }, (_, i) => ({ id: `person-${i}`, kind: 'human', role: '', credential_source: 'identity', token_active: true }))
  fetchMock.mockImplementation(async () => response(200, records))
  renderPage()
  const user = userEvent.setup()
  const table = await screen.findByRole('table')
  if (operation === 'create') {
    await user.click(screen.getByRole('button', { name: 'Create identity' }))
    await user.type(screen.getByLabelText('Identity ID'), issued.id)
  } else {
    await user.click(within(screen.getByText('person-79').closest('tr')!).getByRole('button', { name: 'Rotate Token' }))
  }
  fetchMock.mockResolvedValueOnce(response(201, issued))
  // A failed follow-up refresh must not hide the successfully issued Token.
  fetchMock.mockResolvedValueOnce(response(500))
  await user.click(screen.getByRole('button', { name: operation === 'create' ? 'Create and issue Token' : 'Confirm change' }))
  const result = await screen.findByRole('region', { name: 'Token issued' })
  await waitFor(() => expect(result).toHaveFocus())
  expect(result.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(screen.getByRole('alert').compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(screen.getByLabelText('Identity Token')).toHaveValue(issued.token)
  expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
  expect(within(table).getAllByRole('row')).toHaveLength(81)
})

it.each([
  ['\u0085', false], ['\ufeff', true], ['\u2007\u202f\u3000', false], ['\u0085person\u0085', true],
])('uses Go whitespace semantics for ID %j (valid=%s)', async (id, valid) => {
  renderPage()
  const user = await connect()
  fireEvent.change(screen.getByLabelText('Identity ID'), { target: { value: id } })
  const create = screen.getByRole('button', { name: 'Create and issue Token' })
  if (valid) expect(create).toBeEnabled()
  else expect(create).toBeDisabled()
  if (valid) {
    fetchMock.mockResolvedValueOnce(response(201, { ...issued, id }))
    await user.click(create)
    expect(JSON.parse(fetchMock.mock.calls[1][1]!.body as string).id).toBe(id)
  } else {
    fireEvent.submit(screen.getByLabelText('Identity ID').closest('form')!)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  }
})

it('trims Agent roles using the server whitespace rules', async () => {
  renderPage()
  const user = await connect()
  await user.type(screen.getByLabelText('Identity ID'), 'agent')
  await user.selectOptions(screen.getByLabelText('Identity type'), 'agent')
  fireEvent.change(screen.getByLabelText('Agent role'), { target: { value: '\u0085' } })
  expect(screen.getByRole('button', { name: 'Create and issue Token' })).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Agent role'), { target: { value: '\u0085\ufeffdeveloper\u0085' } })
  fetchMock.mockResolvedValueOnce(response(201, { ...issued, kind: 'agent' }))
  await user.click(screen.getByRole('button', { name: 'Create and issue Token' }))
  expect(JSON.parse(fetchMock.mock.calls[1][1]!.body as string).role).toBe('\ufeffdeveloper')
})
