import type { CreateWorkflowDefinitionInput, WorkflowDefinition, WorkflowRelationDefinition, WorkflowTaskDefinition } from './types'

export interface WorkflowDraft {
  definitionID: string
  baseVersion: number | null
  targetVersion: number
  name: string
  description: string
  agentInstructions: string
  suggestedTags: string[]
  tasks: WorkflowTaskDefinition[]
  relations: WorkflowRelationDefinition[]
  startTaskIDs: string[]
  maxTaskExecutions: number
  savedAt: string
}

const storagePrefix = 'kairos-workflow-draft:'

export function workflowDraftKey(definitionID: string | null, baseVersion: number | null) {
  return definitionID ? `${storagePrefix}${definitionID}:v${baseVersion ?? 0}` : `${storagePrefix}new`
}

export function newWorkflowDraft(): WorkflowDraft {
  return {
    definitionID: crypto.randomUUID(), baseVersion: null, targetVersion: 1,
    name: '', description: '', agentInstructions: '', suggestedTags: [],
    tasks: [], relations: [], startTaskIDs: [], maxTaskExecutions: 20,
    savedAt: new Date().toISOString(),
  }
}

export function draftFromDefinition(definition: WorkflowDefinition): WorkflowDraft {
  return {
    definitionID: definition.id, baseVersion: definition.version, targetVersion: definition.version + 1,
    name: definition.name, description: definition.description, agentInstructions: definition.agent_instructions,
    suggestedTags: [...definition.suggested_tags], tasks: definition.graph.tasks.map(task => ({ ...task, allowed_roles: [...task.allowed_roles], default_tags: [...task.default_tags], artifacts: (task.artifacts ?? []).map(artifact => ({ ...artifact })) })),
    relations: definition.graph.relations.map(relation => ({ ...relation })), startTaskIDs: [...definition.graph.start_task_ids],
    maxTaskExecutions: definition.graph.max_task_executions, savedAt: new Date().toISOString(),
  }
}

export function loadWorkflowDraft(key: string): WorkflowDraft | null {
  try {
    const value = localStorage.getItem(key)
    if (!value) return null
    const draft = JSON.parse(value) as WorkflowDraft
    return { ...draft, tasks: draft.tasks.map(task => ({ ...task, artifacts: task.artifacts ?? [] })), relations: draft.relations.map(relation => ({ ...relation, label: relation.label ?? '', agent_guidance: relation.agent_guidance ?? '' })) }
  } catch { return null }
}

export function saveWorkflowDraft(key: string, draft: WorkflowDraft) {
  localStorage.setItem(key, JSON.stringify({ ...draft, savedAt: new Date().toISOString() }))
}

export function removeWorkflowDraft(key: string) { localStorage.removeItem(key) }

export function appendWorkflowTask(draft: WorkflowDraft, task: WorkflowTaskDefinition) {
  return { ...draft, tasks: [...draft.tasks, task], startTaskIDs: draft.tasks.length === 0 ? [task.id] : draft.startTaskIDs }
}

export function connectWorkflowTasks(draft: WorkflowDraft, from: string, to: string, relationID: string) {
  if (draft.relations.some(relation => relation.from_task_id === from && relation.to_task_id === to)) return draft
  return { ...draft, relations: [...draft.relations, { id: relationID, from_task_id: from, to_task_id: to, label: '', agent_guidance: '' }] }
}

export function deleteWorkflowTask(draft: WorkflowDraft, taskID: string) {
  return { ...draft, tasks: draft.tasks.filter(task => task.id !== taskID), relations: draft.relations.filter(relation => relation.from_task_id !== taskID && relation.to_task_id !== taskID), startTaskIDs: draft.startTaskIDs.filter(id => id !== taskID) }
}

export function toggleWorkflowStartTask(draft: WorkflowDraft, taskID: string) {
  return { ...draft, startTaskIDs: draft.startTaskIDs.includes(taskID) ? draft.startTaskIDs.filter(id => id !== taskID) : [...draft.startTaskIDs, taskID] }
}

export function validateWorkflowDraft(draft: WorkflowDraft) {
  const errors: string[] = []
  if (!draft.name.trim()) errors.push('name')
  if (draft.tasks.length === 0) errors.push('tasks')
  if (draft.startTaskIDs.length === 0) errors.push('start')
  if (draft.tasks.some(task => !task.title.trim())) errors.push('titles')
  if (draft.tasks.some(task => task.artifacts.some(artifact => !artifact.name.trim() || !artifact.description.trim()))) errors.push('artifacts')
  if (draft.tasks.some(task => new Set(task.artifacts.map(artifact => artifact.name.trim())).size !== task.artifacts.length)) errors.push('duplicate-artifacts')
  if (draft.maxTaskExecutions <= 0) errors.push('execution-limit')
  const pairs = new Set<string>()
  for (const relation of draft.relations) {
    const pair = `${relation.from_task_id}:${relation.to_task_id}`
    if (pairs.has(pair)) errors.push('duplicate-relation')
    pairs.add(pair)
  }
  return [...new Set(errors)]
}

export function workflowDraftInput(draft: WorkflowDraft): CreateWorkflowDefinitionInput {
  return {
    id: draft.definitionID, base_version: draft.baseVersion ?? undefined, name: draft.name.trim(), description: draft.description.trim(),
    agent_instructions: draft.agentInstructions.trim(), suggested_tags: draft.suggestedTags,
    graph: {
      start_task_ids: draft.startTaskIDs,
      tasks: draft.tasks.map(task => ({ id: task.id, title: task.title.trim(), description: task.description.trim(), acceptance_criteria: task.acceptance_criteria.trim(), executor: task.executor, allowed_roles: task.allowed_roles, execution: task.execution, review_policy: task.review_policy, default_tags: task.default_tags, artifacts: task.artifacts.map(artifact => ({ name: artifact.name.trim(), description: artifact.description.trim() })) })),
      relations: draft.relations.map(relation => ({ id: relation.id, from_task_id: relation.from_task_id, to_task_id: relation.to_task_id, label: (relation.label ?? '').trim(), agent_guidance: (relation.agent_guidance ?? '').trim() })),
      max_task_executions: draft.maxTaskExecutions,
    },
  }
}
