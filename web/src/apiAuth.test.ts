// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, authenticationRequiredEvent, clearBearerToken, configureAuthenticationMode, saveBearerToken, tokenStorageUnavailableEvent, TokenStorageError } from './api'
import type { Identity } from './types'

const trustedIdentity: Identity = { id: 'local-human', kind: 'human', role: '' }

beforeEach(() => configureAuthenticationMode('authenticated'))

afterEach(() => {
  clearBearerToken()
  configureAuthenticationMode('trusted')
  vi.restoreAllMocks()
})

describe('API authentication transport', () => {
  it('uses the session Token instead of trusted actor headers', async () => {
    saveBearerToken('identity-secret')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [] }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))

    await api.listWorkItems(trustedIdentity)

    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers)
    expect(headers.get('Authorization')).toBe('Bearer identity-secret')
    expect(headers.has('X-Kairos-Actor-Id')).toBe(false)
    expect(headers.has('X-Kairos-Actor-Kind')).toBe(false)
  })

  it('keeps authentication-mode discovery public', async () => {
    saveBearerToken('identity-secret')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { mode: 'authenticated' } }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))

    await api.getAuthenticationConfig()

    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers)
    expect(headers.has('Authorization')).toBe(false)
  })

  it('encodes Definition pagination parameters', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [], next_cursor: null }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))

    await api.listWorkflowDefinitions(trustedIdentity, 'next page', { limit: 1 })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/definitions/workflows?cursor=next+page&limit=1')
  })

  it('uses ID-scoped Definition version routes and omits the path ID from JSON', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response(JSON.stringify({ data: {} }), {
      status: 201, headers: { 'Content-Type': 'application/json' },
    }))

    await api.createDefinition(trustedIdentity, {
      id: 'release/board', name: 'Release', description: '', agent_instructions: '', suggested_tags: [],
    })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/definitions/blackboards/release%2Fboard/versions')
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      name: 'Release', description: '', agent_instructions: '', suggested_tags: [],
    })

    await api.createDefinition(trustedIdentity, {
      id: 'release/board', base_version: 2, name: 'Release v3', description: '', agent_instructions: '', suggested_tags: [],
    })
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      base_version: 2, name: 'Release v3', description: '', agent_instructions: '', suggested_tags: [],
    })
  })

  it('encodes repeated WorkItem status filters with its cursor', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [], next_cursor: null }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))

    await api.listWorkItems(trustedIdentity, 'active-page', { statuses: ['open', 'awaiting_human_acceptance'] })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/work-items?cursor=active-page&status=open&status=awaiting_human_acceptance')
  })

  it('does not access Token storage in Trusted Mode', async () => {
    configureAuthenticationMode('trusted')
    const originalGetItem = Storage.prototype.getItem
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, key) {
      if (this === sessionStorage) throw new DOMException('Access denied', 'SecurityError')
      return originalGetItem.call(this, key)
    })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [] }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))

    await api.listWorkItems(trustedIdentity)

    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers)
    expect(headers.get('X-Kairos-Actor-Id')).toBe('local-human')
    expect(headers.has('Authorization')).toBe(false)
  })

  it('reports Token storage failures from work requests', async () => {
    const originalGetItem = Storage.prototype.getItem
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, key) {
      if (this === sessionStorage) throw new DOMException('Access denied', 'SecurityError')
      return originalGetItem.call(this, key)
    })
    const storageUnavailable = vi.fn()
    window.addEventListener(tokenStorageUnavailableEvent, storageUnavailable, { once: true })

    await expect(api.listWorkItems(trustedIdentity)).rejects.toBeInstanceOf(TokenStorageError)

    expect(storageUnavailable).toHaveBeenCalledTimes(1)
  })

  it('reports a rejected authenticated request to the application gate', async () => {
    saveBearerToken('revoked-secret')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { message: 'unauthenticated' } }), {
      status: 401, headers: { 'Content-Type': 'application/json' },
    }))
    const authenticationRequired = vi.fn()
    window.addEventListener(authenticationRequiredEvent, authenticationRequired, { once: true })

    await expect(api.listWorkItems(trustedIdentity)).rejects.toMatchObject({ status: 401 })

    expect(authenticationRequired).toHaveBeenCalledTimes(1)
  })

  it('ignores a late 401 from a superseded Token', async () => {
    saveBearerToken('old-secret')
    let resolveResponse: (response: Response) => void = () => undefined
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(resolve => { resolveResponse = resolve }))
    const authenticationRequired = vi.fn()
    window.addEventListener(authenticationRequiredEvent, authenticationRequired, { once: true })

    const request = api.listWorkItems(trustedIdentity)
    saveBearerToken('new-secret')
    resolveResponse(new Response(JSON.stringify({ error: { message: 'unauthenticated' } }), {
      status: 401, headers: { 'Content-Type': 'application/json' },
    }))

    await expect(request).rejects.toMatchObject({ status: 401 })
    expect(authenticationRequired).not.toHaveBeenCalled()
    window.removeEventListener(authenticationRequiredEvent, authenticationRequired)
  })
})
