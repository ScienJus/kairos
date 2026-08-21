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
    definitionID: definition.ID, baseVersion: definition.Version, targetVersion: definition.Version + 1,
    name: definition.Name, description: definition.Description, agentInstructions: definition.AgentInstructions,
    suggestedTags: [...definition.SuggestedTags], tasks: definition.Graph.Tasks.map(task => ({ ...task, AllowedRoles: [...task.AllowedRoles], DefaultTags: [...task.DefaultTags] })),
    relations: definition.Graph.Relations.map(relation => ({ ...relation })), startTaskIDs: [...definition.Graph.StartTaskIDs],
    maxTaskExecutions: definition.Graph.MaxTaskExecutions, savedAt: new Date().toISOString(),
  }
}

export function loadWorkflowDraft(key: string): WorkflowDraft | null {
  try {
    const value = localStorage.getItem(key)
    if (!value) return null
    const draft = JSON.parse(value) as WorkflowDraft
    return { ...draft, relations: draft.relations.map(relation => ({ ...relation, Label: relation.Label ?? '', AgentGuidance: relation.AgentGuidance ?? '' })) }
  } catch { return null }
}

export function saveWorkflowDraft(key: string, draft: WorkflowDraft) {
  localStorage.setItem(key, JSON.stringify({ ...draft, savedAt: new Date().toISOString() }))
}

export function removeWorkflowDraft(key: string) { localStorage.removeItem(key) }

export function appendWorkflowTask(draft: WorkflowDraft, task: WorkflowTaskDefinition) {
  return { ...draft, tasks: [...draft.tasks, task], startTaskIDs: draft.tasks.length === 0 ? [task.ID] : draft.startTaskIDs }
}

export function connectWorkflowTasks(draft: WorkflowDraft, from: string, to: string, relationID: string) {
  if (draft.relations.some(relation => relation.FromTaskID === from && relation.ToTaskID === to)) return draft
  return { ...draft, relations: [...draft.relations, { ID: relationID, FromTaskID: from, ToTaskID: to, Label: '', AgentGuidance: '' }] }
}

export function deleteWorkflowTask(draft: WorkflowDraft, taskID: string) {
  return { ...draft, tasks: draft.tasks.filter(task => task.ID !== taskID), relations: draft.relations.filter(relation => relation.FromTaskID !== taskID && relation.ToTaskID !== taskID), startTaskIDs: draft.startTaskIDs.filter(id => id !== taskID) }
}

export function toggleWorkflowStartTask(draft: WorkflowDraft, taskID: string) {
  return { ...draft, startTaskIDs: draft.startTaskIDs.includes(taskID) ? draft.startTaskIDs.filter(id => id !== taskID) : [...draft.startTaskIDs, taskID] }
}

export function validateWorkflowDraft(draft: WorkflowDraft) {
  const errors: string[] = []
  if (!draft.name.trim()) errors.push('name')
  if (draft.tasks.length === 0) errors.push('tasks')
  if (draft.startTaskIDs.length === 0) errors.push('start')
  if (draft.tasks.some(task => !task.Title.trim())) errors.push('titles')
  if (draft.maxTaskExecutions <= 0) errors.push('execution-limit')
  const pairs = new Set<string>()
  for (const relation of draft.relations) {
    const pair = `${relation.FromTaskID}:${relation.ToTaskID}`
    if (pairs.has(pair)) errors.push('duplicate-relation')
    pairs.add(pair)
  }
  return [...new Set(errors)]
}

export function workflowDraftInput(draft: WorkflowDraft): CreateWorkflowDefinitionInput {
  return {
    id: draft.definitionID, version: draft.targetVersion, name: draft.name.trim(), description: draft.description.trim(),
    agent_instructions: draft.agentInstructions.trim(), suggested_tags: draft.suggestedTags, status: 'published',
    graph: {
      start_task_ids: draft.startTaskIDs,
      tasks: draft.tasks.map(task => ({ id: task.ID, title: task.Title.trim(), description: task.Description.trim(), acceptance_criteria: task.AcceptanceCriteria.trim(), executor: task.Executor, allowed_roles: task.AllowedRoles, execution: task.Execution, review_policy: task.ReviewPolicy, default_tags: task.DefaultTags })),
      relations: draft.relations.map(relation => ({ id: relation.ID, from_task_id: relation.FromTaskID, to_task_id: relation.ToTaskID, label: (relation.Label ?? '').trim(), agent_guidance: (relation.AgentGuidance ?? '').trim() })),
      max_task_executions: draft.maxTaskExecutions,
    },
  }
}
