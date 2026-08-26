import type { Artifact, AuthenticationConfig, BlackboardTaskDecomposition, Claim, CreateDefinitionInput, CreateWorkflowDefinitionInput, CreateWorkItemInput, DecomposeTaskInput, Definition, FailTaskInput, HumanAttentionItem, Identity, ReviewDecisionInput, Submission, SubmitTaskInput, Task, TaskDetailView, TaskDraftInput, TaskExecutionContext, WorkflowDefinition, WorkItem, WorkItemContext } from './types'

const identityKey = 'kairos-console-identity'
const bearerTokenKey = 'kairos-console-token'
export const authenticationRequiredEvent = 'kairos:authentication-required'
export const tokenStorageUnavailableEvent = 'kairos:token-storage-unavailable'
let authenticationMode: AuthenticationConfig['mode'] = 'trusted'

export class TokenStorageError extends Error {
  constructor() { super('Browser session storage is unavailable') }
}

export function configureAuthenticationMode(mode: AuthenticationConfig['mode']) {
  authenticationMode = mode
}

function tokenStorageError() {
  window.dispatchEvent(new Event(tokenStorageUnavailableEvent))
  return new TokenStorageError()
}

export function loadIdentity(): Identity {
  try {
    const stored = localStorage.getItem(identityKey)
    if (stored) {
      const value = JSON.parse(stored) as { id?: unknown }
      if (typeof value.id === 'string' && value.id.trim()) return { id: value.id, kind: 'human', role: '' }
    }
  } catch { /* use defaults */ }
  return { id: 'kairos-operator', kind: 'human', role: '' }
}

export function saveIdentity(identity: Identity) {
  localStorage.setItem(identityKey, JSON.stringify(identity))
}

export function loadBearerToken() {
  try {
    return sessionStorage.getItem(bearerTokenKey) ?? ''
  } catch {
    throw tokenStorageError()
  }
}

export function saveBearerToken(token: string) {
  try {
    sessionStorage.setItem(bearerTokenKey, token)
  } catch {
    throw tokenStorageError()
  }
}

export function clearBearerToken() {
  try {
    sessionStorage.removeItem(bearerTokenKey)
  } catch {
    throw tokenStorageError()
  }
}

export class APIError extends Error {
  constructor(public status: number, message: string, public code = '') { super(message) }
}

export interface Page<T> {
  data: T[]
  next_cursor: string | null
}

function authenticationHeaders(identity?: Identity) {
  const headers = new Headers()
  const token = authenticationMode === 'authenticated' ? loadBearerToken() : ''
  if (authenticationMode === 'authenticated' && token) {
    headers.set('Authorization', `Bearer ${token}`)
  } else if (identity) {
    headers.set('X-Kairos-Actor-Id', identity.id)
    headers.set('X-Kairos-Actor-Kind', identity.kind)
  }
  return { headers, token }
}

function handleUnauthorized(status: number, requestToken: string) {
  if (status !== 401 || !requestToken) return
  try {
    if (loadBearerToken() === requestToken) window.dispatchEvent(new Event(authenticationRequiredEvent))
  } catch { /* the authentication gate reports storage failures on its next operation */ }
}

async function requestJSON<T>(path: string, identity?: Identity, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  const authentication = authenticationHeaders(identity)
  headers.set('Accept', 'application/json')
  authentication.headers.forEach((value, name) => headers.set(name, value))
  if (init?.body && !(init.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...init, headers })
  if (!response.ok) {
    handleUnauthorized(response.status, authentication.token)
    const body = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null
    throw new APIError(response.status, body?.error?.message ?? `Request failed (${response.status})`, body?.error?.code)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function request<T>(path: string, identity?: Identity, init?: RequestInit): Promise<T> {
  const body = await requestJSON<{ data: T }>(path, identity, init)
  return body.data
}

const pendingCreationKeys = new Map<string, string>()

async function createResource<T>(path: string, identity: Identity, body?: string): Promise<T> {
  const requestKey = JSON.stringify([identity.kind, identity.id, identity.role, path, body ?? ''])
  const operationID = pendingCreationKeys.get(requestKey) ?? crypto.randomUUID()
  pendingCreationKeys.set(requestKey, operationID)
  try {
    const result = await request<T>(path, identity, {
      method: 'POST', body, headers: { 'Idempotency-Key': operationID },
    })
    pendingCreationKeys.delete(requestKey)
    return result
  } catch (error) {
    // A 4xx is a definitive rejection. Network failures and 5xx responses may
    // still hide an upstream commit, so the next identical call reuses the key.
    if (error instanceof APIError && error.status < 500) pendingCreationKeys.delete(requestKey)
    throw error
  }
}

function pagePath(path: string, cursor?: string, parameters?: Record<string, string | number | string[] | undefined>) {
  const query = new URLSearchParams()
  if (cursor) query.set('cursor', cursor)
  Object.entries(parameters ?? {}).forEach(([name, value]) => {
    if (Array.isArray(value)) value.forEach(item => query.append(name, item))
    else if (value !== undefined) query.set(name, String(value))
  })
  const encoded = query.toString()
  return encoded ? `${path}?${encoded}` : path
}

function requestPage<T>(path: string, identity: Identity, cursor?: string, parameters?: Record<string, string | number | string[] | undefined>) {
  return requestJSON<Page<T>>(pagePath(path, cursor, parameters), identity)
}

export const api = {
  getAuthenticationConfig: async () => {
    const response = await fetch('/api/v1/auth/config', { cache: 'no-store', headers: { Accept: 'application/json' } })
    if (!response.ok) throw new APIError(response.status, `Request failed (${response.status})`)
    const body = await response.json() as { data: AuthenticationConfig }
    return body.data
  },
  getSession: (identity?: Identity) => request<Identity>('/api/v1/session', identity, { cache: 'no-store' }),
  listWorkItems: (identity: Identity, cursor?: string, options?: { statuses?: WorkItem['status'][] }) => requestPage<WorkItem>('/api/v1/work-items', identity, cursor, { status: options?.statuses }),
  listHumanAttention: (identity: Identity, cursor?: string) => requestPage<HumanAttentionItem>('/api/v1/human-attention', identity, cursor),
  getWorkItem: (identity: Identity, id: string) => request<WorkItemContext>(`/api/v1/work-items/${id}/context`, identity),
  getTaskContext: (identity: Identity, id: string) => request<TaskExecutionContext>(`/api/v1/tasks/${id}/context`, identity),
  getTaskDetail: (identity: Identity, id: string) => request<TaskDetailView>(`/api/v1/tasks/${id}`, identity),
  listBlackboardDefinitions: (identity: Identity, cursor?: string, options?: { limit?: number }) => requestPage<Definition>('/api/v1/definitions/blackboards', identity, cursor, options),
  listWorkflowDefinitions: (identity: Identity, cursor?: string, options?: { limit?: number }) => requestPage<WorkflowDefinition>('/api/v1/definitions/workflows', identity, cursor, options),
  listBlackboardDefinitionVersions: (identity: Identity, id: string, cursor?: string) => requestPage<Definition>(`/api/v1/definitions/blackboards/${encodeURIComponent(id)}/versions`, identity, cursor),
  listWorkflowDefinitionVersions: (identity: Identity, id: string, cursor?: string) => requestPage<WorkflowDefinition>(`/api/v1/definitions/workflows/${encodeURIComponent(id)}/versions`, identity, cursor),
  getLatestBlackboardDefinition: (identity: Identity, id: string) => request<Definition>(`/api/v1/definitions/blackboards/${encodeURIComponent(id)}`, identity),
  getLatestWorkflowDefinition: (identity: Identity, id: string) => request<WorkflowDefinition>(`/api/v1/definitions/workflows/${encodeURIComponent(id)}`, identity),
  getBlackboardDefinition: (identity: Identity, id: string, version: number) => request<Definition>(`/api/v1/definitions/blackboards/${encodeURIComponent(id)}/versions/${version}`, identity),
  getWorkflowDefinition: (identity: Identity, id: string, version: number) => request<WorkflowDefinition>(`/api/v1/definitions/workflows/${encodeURIComponent(id)}/versions/${version}`, identity),
  createDefinition: (identity: Identity, input: CreateDefinitionInput) => {
    const { id, ...version } = input
    return request<Definition>(`/api/v1/definitions/blackboards/${encodeURIComponent(id)}/versions`, identity, { method: 'POST', body: JSON.stringify(version) })
  },
  createWorkflowDefinition: (identity: Identity, input: CreateWorkflowDefinitionInput) => {
    const { id, ...version } = input
    return request<WorkflowDefinition>(`/api/v1/definitions/workflows/${encodeURIComponent(id)}/versions`, identity, { method: 'POST', body: JSON.stringify(version) })
  },
  createWorkItem: (identity: Identity, input: CreateWorkItemInput) => createResource<WorkItem>('/api/v1/work-items', identity, JSON.stringify(input)),
  createTask: (identity: Identity, workItemID: string, input: TaskDraftInput) => createResource<Task>(`/api/v1/work-items/${workItemID}/tasks`, identity, JSON.stringify(input)),
  submitBlackboardCompletion: (identity: Identity, workItemID: string, result: string) => request<WorkItem>(`/api/v1/work-items/${workItemID}/completion`, identity, {
    method: 'POST', body: JSON.stringify({ result }),
  }),
  acceptBlackboardCompletion: (identity: Identity, workItemID: string) => request<WorkItem>(`/api/v1/work-items/${workItemID}/acceptance`, identity, { method: 'POST' }),
  cancelWorkItem: (identity: Identity, workItemID: string, reason: string) => request<WorkItem>(`/api/v1/work-items/${workItemID}/cancellation`, identity, {
    method: 'POST', body: JSON.stringify({ reason }),
  }),
  claimTask: (identity: Identity, taskID: string) => createResource<Claim>(`/api/v1/tasks/${taskID}/claims`, identity),
  releaseClaim: (identity: Identity, taskID: string, claimID: string) => request<void>(`/api/v1/tasks/${taskID}/claims/${claimID}`, identity, { method: 'DELETE' }),
  createArtifact: (identity: Identity, taskID: string, input: { claim_id: string; name: string; uri: string }) => createResource<Artifact>(`/api/v1/tasks/${taskID}/artifacts`, identity, JSON.stringify(input)),
  uploadArtifact: (identity: Identity, taskID: string, claimID: string, name: string, file: File, operationID: string) => {
    const form = new FormData()
    form.set('claim_id', claimID); form.set('name', name); form.set('file', file)
    return request<Artifact>(`/api/v1/tasks/${taskID}/artifact-uploads`, identity, {
      method: 'POST', body: form, headers: { 'Idempotency-Key': operationID },
    })
  },
  downloadArtifact: async (identity: Identity, artifactID: string) => {
    const authentication = authenticationHeaders(identity)
    const response = await fetch(`/api/v1/artifacts/${artifactID}/content`, { headers: authentication.headers })
    if (!response.ok) {
      handleUnauthorized(response.status, authentication.token)
      throw new APIError(response.status, `Request failed (${response.status})`)
    }
    return response.blob()
  },
  submitTask: (identity: Identity, taskID: string, input: SubmitTaskInput) => request<Submission>(`/api/v1/tasks/${taskID}/submissions`, identity, { method: 'POST', body: JSON.stringify(input) }),
  failTask: (identity: Identity, taskID: string, input: FailTaskInput) => request(`/api/v1/tasks/${taskID}/failures`, identity, { method: 'POST', body: JSON.stringify(input) }),
  skipBlackboardTask: (identity: Identity, taskID: string, reason: string) => request<Task>(`/api/v1/tasks/${taskID}/skip`, identity, {
    method: 'POST', body: JSON.stringify({ reason }),
  }),
  decomposeBlackboardTask: (identity: Identity, taskID: string, input: DecomposeTaskInput) => createResource<BlackboardTaskDecomposition>(`/api/v1/tasks/${taskID}/decomposition`, identity, JSON.stringify(input)),
  addBlackboardChildTask: (identity: Identity, taskID: string, input: TaskDraftInput) => createResource<Task>(`/api/v1/tasks/${taskID}/children`, identity, JSON.stringify(input)),
  decideReview: (identity: Identity, taskID: string, reviewID: string, input: ReviewDecisionInput) => request(`/api/v1/tasks/${taskID}/reviews/${reviewID}/decision`, identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
}
