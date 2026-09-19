import { APIError, authenticationRequiredEvent, loadBearerToken } from './api'
import type { CreateIdentityInput, IdentityRecord, IssuedIdentityToken } from './types'

// Results stay in page memory, outside Query/Mutation caches. Use the current
// login credential, never infer administrator access from an actor ID or label.
async function adminRequest(path: string, signal: AbortSignal, method = 'GET', input?: CreateIdentityInput): Promise<Response> {
  const token = loadBearerToken()
  const response = await fetch(`/api/v1/identities${path}`, {
    method,
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(input ? { 'Content-Type': 'application/json' } : {}) },
    body: input ? JSON.stringify(input) : undefined,
    signal, cache: 'no-store', redirect: 'error', credentials: 'omit',
  })
  if (response.status === 401 && !signal.aborted && token === loadBearerToken()) window.dispatchEvent(new Event(authenticationRequiredEvent))
  // Do not render arbitrary server messages, which could echo submitted data.
  if (!response.ok) throw new APIError(response.status, 'Admin request failed')
  return response
}

export async function listIdentities(signal: AbortSignal): Promise<IdentityRecord[]> {
  const response = await adminRequest('', signal)
  return (await response.json() as { data: IdentityRecord[] }).data
}

export async function createIdentity(input: CreateIdentityInput, signal: AbortSignal): Promise<IssuedIdentityToken> {
  const response = await adminRequest('', signal, 'POST', input)
  return (await response.json() as { data: IssuedIdentityToken }).data
}

export async function changeIdentityToken(identity: IdentityRecord, action: 'rotate' | 'revoke', signal: AbortSignal): Promise<IssuedIdentityToken | null> {
  const path = `/${encodeURIComponent(identity.kind)}/${encodeURIComponent(identity.id)}/token`
  const response = await adminRequest(path, signal, action === 'rotate' ? 'POST' : 'DELETE')
  return action === 'revoke' ? null : (await response.json() as { data: IssuedIdentityToken }).data
}
