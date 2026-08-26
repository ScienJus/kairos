// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { APIError, api, authenticationRequiredEvent, configureAuthenticationMode, tokenStorageUnavailableEvent } from './api'
import { I18nProvider } from './i18n'
import type { Identity } from './types'

const authenticatedIdentity: Identity = { id: 'console-human', kind: 'human', role: '' }

function renderApp() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><App /></I18nProvider></QueryClientProvider>)
}

beforeEach(() => {
  configureAuthenticationMode('trusted')
  localStorage.setItem('kairos-console-locale', 'en')
  sessionStorage.clear()
  window.history.replaceState({}, '', '/')
  vi.spyOn(api, 'listWorkItems').mockResolvedValue({ data: [], next_cursor: null })
  vi.spyOn(api, 'listHumanAttention').mockResolvedValue({ data: [], next_cursor: null })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
  sessionStorage.clear()
})

describe('console authentication', () => {
  it('shows Token login before the workspace in Authenticated Mode', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    const session = vi.spyOn(api, 'getSession')
    renderApp()

    expect(await screen.findByRole('heading', { name: 'Sign in to Kairos' })).toBeInTheDocument()
    expect(screen.queryByText('Your work, held clearly.')).not.toBeInTheDocument()
    expect(session).not.toHaveBeenCalled()
  })

  it('validates and stores a Token before entering the workspace', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    const session = vi.spyOn(api, 'getSession').mockResolvedValue(authenticatedIdentity)
    const user = userEvent.setup()
    renderApp()

    await user.type(await screen.findByLabelText('Identity Token'), 'identity-secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByTitle('Authenticated as: console-human')).toBeInTheDocument()
    expect(session).toHaveBeenCalledTimes(1)
    expect(sessionStorage.getItem('kairos-console-token')).toBe('identity-secret')
  })

  it('rejects an invalid Token without retaining it', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockRejectedValue(new APIError(401, 'unauthenticated'))
    const user = userEvent.setup()
    renderApp()

    await user.type(await screen.findByLabelText('Identity Token'), 'revoked-secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('This Token is invalid or has been revoked.')
    expect(sessionStorage.getItem('kairos-console-token')).toBeNull()
  })

  it('restores a saved Token and supports logout', async () => {
    sessionStorage.setItem('kairos-console-token', 'saved-secret')
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockResolvedValue(authenticatedIdentity)
    const user = userEvent.setup()
    renderApp()

    await user.click(await screen.findByLabelText('Authenticated as: console-human'))
    await user.click(screen.getByRole('button', { name: 'Sign out' }))

    expect(await screen.findByRole('heading', { name: 'Sign in to Kairos' })).toBeInTheDocument()
    expect(sessionStorage.getItem('kairos-console-token')).toBeNull()
  })

  it('keeps a saved Token when session verification cannot reach the server', async () => {
    sessionStorage.setItem('kairos-console-token', 'saved-secret')
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockRejectedValue(new TypeError('Failed to fetch'))
    renderApp()

    expect(await screen.findByText('Kairos could not verify the saved session. The Token has been kept; check the server connection and try again.')).toBeInTheDocument()
    expect(sessionStorage.getItem('kairos-console-token')).toBe('saved-secret')
  })

  it('reports unavailable session storage instead of remaining in loading', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    const originalGetItem = Storage.prototype.getItem
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, key) {
      if (this === sessionStorage) throw new DOMException('Access denied', 'SecurityError')
      return originalGetItem.call(this, key)
    })
    renderApp()

    expect(await screen.findByText('This browser is not allowing access to session storage. Enable session storage to use the Kairos console.')).toBeInTheDocument()
  })

  it('reports a session storage write failure during login', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    const originalSetItem = Storage.prototype.setItem
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, key, value) {
      if (this === sessionStorage) throw new DOMException('Access denied', 'SecurityError')
      return originalSetItem.call(this, key, value)
    })
    const user = userEvent.setup()
    renderApp()

    await user.type(await screen.findByLabelText('Identity Token'), 'identity-secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('This browser is not allowing access to session storage. Enable session storage to use the Kairos console.')).toBeInTheDocument()
  })

  it('dismisses the account menu on outside interaction and Escape', async () => {
    sessionStorage.setItem('kairos-console-token', 'saved-secret')
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockResolvedValue(authenticatedIdentity)
    const user = userEvent.setup()
    renderApp()

    const account = await screen.findByLabelText('Authenticated as: console-human')
    await user.click(account)
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
    await user.click(document.body)
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument()

    await user.click(account)
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument()
    expect(account).toHaveFocus()

    await user.click(account)
    window.dispatchEvent(new PopStateEvent('popstate'))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument())
  })

  it('returns to login when an authenticated request reports an invalid session', async () => {
    sessionStorage.setItem('kairos-console-token', 'saved-secret')
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockResolvedValue(authenticatedIdentity)
    renderApp()

    await screen.findByTitle('Authenticated as: console-human')
    window.dispatchEvent(new Event(authenticationRequiredEvent))

    expect(await screen.findByRole('alert')).toHaveTextContent('Your session is no longer valid. Sign in again.')
    expect(sessionStorage.getItem('kairos-console-token')).toBeNull()
  })

  it('reports Token storage becoming unavailable after login', async () => {
    sessionStorage.setItem('kairos-console-token', 'saved-secret')
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'authenticated' })
    vi.spyOn(api, 'getSession').mockResolvedValue(authenticatedIdentity)
    renderApp()

    await screen.findByTitle('Authenticated as: console-human')
    window.dispatchEvent(new Event(tokenStorageUnavailableEvent))

    expect(await screen.findByText('This browser is not allowing access to session storage. Enable session storage to use the Kairos console.')).toBeInTheDocument()
  })

  it('keeps the editable local identity in Trusted Mode', async () => {
    vi.spyOn(api, 'getAuthenticationConfig').mockResolvedValue({ mode: 'trusted' })
    const session = vi.spyOn(api, 'getSession')
    renderApp()

    expect(await screen.findByRole('button', { name: 'Identity settings' })).toBeInTheDocument()
    await waitFor(() => expect(session).not.toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument()
  })
})
