// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { WorkflowDefinition, WorkflowTaskDefinition } from './types'
import { appendWorkflowTask, connectWorkflowTasks, deleteWorkflowTask, draftFromDefinition, loadWorkflowDraft, newWorkflowDraft, saveWorkflowDraft, toggleWorkflowStartTask, validateWorkflowDraft, workflowDraftInput, workflowDraftKey } from './workflowDraft'

const task = (ID: string): WorkflowTaskDefinition => ({ ID, Title: ID, Description: '', AcceptanceCriteria: '', Executor: 'agent', AllowedRoles: [], Execution: 'required', ReviewPolicy: 'none', DefaultTags: [] })
const definition: WorkflowDefinition = {
  ID: 'release', Version: 2, Name: 'Release', Description: '', AgentInstructions: '', SuggestedTags: [], Status: 'published',
  Graph: { StartTaskIDs: ['a'], Tasks: [task('a'), task('b')], Relations: [{ ID: 'ab', FromTaskID: 'a', ToTaskID: 'b' }], MaxTaskExecutions: 10 },
}

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('crypto', { randomUUID: () => 'draft-id' })
})

describe('Workflow local drafts', () => {
  it('creates and restores isolated drafts', () => {
    const fresh = newWorkflowDraft()
    fresh.name = 'Local workflow'
    const key = workflowDraftKey(null, null)
    saveWorkflowDraft(key, fresh)

    expect(fresh.definitionID).toBe('draft-id')
    expect(loadWorkflowDraft(key)?.name).toBe('Local workflow')
    expect(workflowDraftKey('release', 2)).toBe('kairos-workflow-draft:release:v2')
  })

  it('copies a published version into the next version without sharing collections', () => {
    const draft = draftFromDefinition(definition)
    draft.tasks[0].AllowedRoles.push('backend')

    expect(draft.baseVersion).toBe(2)
    expect(draft.targetVersion).toBe(3)
    expect(definition.Graph.Tasks[0].AllowedRoles).toEqual([])
  })

  it('maintains tasks, starts, and relations as one graph operation', () => {
    let draft = newWorkflowDraft()
    draft = appendWorkflowTask(draft, task('a'))
    draft = appendWorkflowTask(draft, task('b'))
    draft = connectWorkflowTasks(draft, 'a', 'b', 'ab')
    draft = connectWorkflowTasks(draft, 'a', 'b', 'duplicate')

    expect(draft.startTaskIDs).toEqual(['a'])
    expect(draft.relations).toHaveLength(1)
    expect(toggleWorkflowStartTask(draft, 'b').startTaskIDs).toEqual(['a', 'b'])
    expect(deleteWorkflowTask(draft, 'a')).toMatchObject({ tasks: [{ ID: 'b' }], relations: [], startTaskIDs: [] })
  })

  it('preserves a self relation because the backend treats it as a workflow cycle', () => {
    let draft = appendWorkflowTask(newWorkflowDraft(), task('a'))
    draft.name = 'Retryable task'
    draft = connectWorkflowTasks(draft, 'a', 'a', 'retry')

    expect(validateWorkflowDraft(draft)).toEqual([])
    expect(workflowDraftInput(draft).graph.relations).toEqual([
      { id: 'retry', from_task_id: 'a', to_task_id: 'a' },
    ])
  })

  it('validates simple client rules and maps the publish contract', () => {
    const empty = newWorkflowDraft()
    expect(validateWorkflowDraft(empty)).toEqual(['name', 'tasks', 'start'])

    const draft = draftFromDefinition(definition)
    const input = workflowDraftInput(draft)
    expect(validateWorkflowDraft(draft)).toEqual([])
    expect(input).toMatchObject({ id: 'release', version: 3, status: 'published', graph: { start_task_ids: ['a'], max_task_executions: 10 } })
    expect(input.graph.relations).toEqual([{ id: 'ab', from_task_id: 'a', to_task_id: 'b' }])
  })
})
