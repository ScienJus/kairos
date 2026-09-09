import { APIError } from './api'
import type { CreateIdentityInput, IssuedIdentityToken } from './types'

// Explicit admin credentials only: no identity headers, storage, global 401
// events, retries, or Query/Mutation caches may handle these secrets.
async function adminRequest(token: string, signal: AbortSignal, input?: CreateIdentityInput): Promise<Response> {
  const response = await fetch('/api/v1/identities', {
    method: input ? 'POST' : 'GET',
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(input ? { 'Content-Type': 'application/json' } : {}) },
    body: input ? JSON.stringify(input) : undefined,
    signal, cache: 'no-store', redirect: 'error', credentials: 'omit',
  })
  // Do not render arbitrary server messages, which could echo submitted data.
  if (!response.ok) throw new APIError(response.status, 'Admin request failed')
  return response
}

export async function verifyAdmin(token: string, signal: AbortSignal): Promise<void> {
  await adminRequest(token, signal)
}

export async function createIdentity(token: string, input: CreateIdentityInput, signal: AbortSignal): Promise<IssuedIdentityToken> {
  const response = await adminRequest(token, signal, input)
  const body = await response.json() as { data: IssuedIdentityToken }
  return body.data
}
