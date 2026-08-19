import type { BlackboardTaskDecomposition, Claim, CreateDefinitionInput, CreateWorkflowDefinitionInput, CreateWorkItemInput, DecomposeTaskInput, Definition, FailTaskInput, HumanAttentionItem, Identity, Mode, ReviewDecisionInput, Submission, SubmitTaskInput, Task, TaskDetailView, TaskDraftInput, TaskExecutionContext, WorkflowDefinition, WorkItem, WorkItemContext } from './types'

const identityKey = 'kairos-console-identity'

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

export class APIError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

async function request<T>(path: string, identity: Identity, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  headers.set('Accept', 'application/json')
  headers.set('X-Kairos-Actor-Id', identity.id)
  headers.set('X-Kairos-Actor-Kind', identity.kind)
  if (init?.body) headers.set('Content-Type', 'application/json')
  if (init?.method && init.method !== 'GET') headers.set('Idempotency-Key', crypto.randomUUID())
  const response = await fetch(path, { ...init, headers })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: { message?: string } } | null
    throw new APIError(response.status, body?.error?.message ?? `Request failed (${response.status})`)
  }
  if (response.status === 204) return undefined as T
  const body = await response.json() as { data: T }
  return body.data
}

export const api = {
  listWorkItems: (identity: Identity) => request<WorkItem[]>('/api/v1/work-items', identity),
  listHumanAttention: (identity: Identity) => request<HumanAttentionItem[]>('/api/v1/human-attention', identity),
  getWorkItem: (identity: Identity, id: string) => request<WorkItemContext>(`/api/v1/work-items/${id}/context`, identity),
  getTaskContext: (identity: Identity, id: string) => request<TaskExecutionContext>(`/api/v1/tasks/${id}/context`, identity),
  getTaskDetail: (identity: Identity, id: string) => request<TaskDetailView>(`/api/v1/tasks/${id}`, identity),
  listBlackboardDefinitions: (identity: Identity) => request<Definition[]>('/api/v1/definitions/blackboards', identity),
  listWorkflowDefinitions: (identity: Identity) => request<WorkflowDefinition[]>('/api/v1/definitions/workflows', identity),
  getWorkflowDefinition: (identity: Identity, id: string, version: number) => request<WorkflowDefinition>(`/api/v1/definitions/workflows/${encodeURIComponent(id)}/versions/${version}`, identity),
  listDefinitions: async (identity: Identity) => {
    const [blackboards, workflows] = await Promise.all([
      request<Definition[]>('/api/v1/definitions/blackboards', identity),
      request<Definition[]>('/api/v1/definitions/workflows', identity),
    ])
    return [
      ...blackboards.map(definition => ({ ...definition, mode: 'blackboard' as Mode })),
      ...workflows.map(definition => ({ ...definition, mode: 'workflow' as Mode })),
    ]
  },
  createDefinition: (identity: Identity, input: CreateDefinitionInput) => request<Definition>('/api/v1/definitions/blackboards', identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  createWorkflowDefinition: (identity: Identity, input: CreateWorkflowDefinitionInput) => request<WorkflowDefinition>('/api/v1/definitions/workflows', identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  createWorkItem: (identity: Identity, input: CreateWorkItemInput) => request<WorkItem>('/api/v1/work-items', identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  createTask: (identity: Identity, workItemID: string, input: TaskDraftInput) => request<Task>(`/api/v1/work-items/${workItemID}/tasks`, identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  completeBlackboard: (identity: Identity, workItemID: string, result: string) => request<WorkItem>(`/api/v1/work-items/${workItemID}/completion`, identity, {
    method: 'POST', body: JSON.stringify({ result }),
  }),
  claimTask: (identity: Identity, taskID: string) => request<Claim>(`/api/v1/tasks/${taskID}/claims`, identity, { method: 'POST' }),
  releaseClaim: (identity: Identity, taskID: string, claimID: string) => request<void>(`/api/v1/tasks/${taskID}/claims/${claimID}`, identity, { method: 'DELETE' }),
  submitTask: (identity: Identity, taskID: string, input: SubmitTaskInput) => request<Submission>(`/api/v1/tasks/${taskID}/submissions`, identity, { method: 'POST', body: JSON.stringify(input) }),
  failTask: (identity: Identity, taskID: string, input: FailTaskInput) => request(`/api/v1/tasks/${taskID}/failures`, identity, { method: 'POST', body: JSON.stringify(input) }),
  skipBlackboardTask: (identity: Identity, taskID: string, reason: string) => request<Task>(`/api/v1/tasks/${taskID}/skip`, identity, {
    method: 'POST', body: JSON.stringify({ reason }),
  }),
  decomposeBlackboardTask: (identity: Identity, taskID: string, input: DecomposeTaskInput) => request<BlackboardTaskDecomposition>(`/api/v1/tasks/${taskID}/decomposition`, identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  addBlackboardChildTask: (identity: Identity, taskID: string, input: TaskDraftInput) => request<Task>(`/api/v1/tasks/${taskID}/children`, identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
  decideReview: (identity: Identity, taskID: string, reviewID: string, input: ReviewDecisionInput) => request(`/api/v1/tasks/${taskID}/reviews/${reviewID}/decision`, identity, {
    method: 'POST', body: JSON.stringify(input),
  }),
}
