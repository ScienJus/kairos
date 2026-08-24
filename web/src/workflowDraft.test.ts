// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { WorkflowDefinition, WorkflowTaskDefinition } from './types'
import { appendWorkflowTask, connectWorkflowTasks, deleteWorkflowTask, draftFromDefinition, loadWorkflowDraft, newWorkflowDraft, saveWorkflowDraft, toggleWorkflowStartTask, validateWorkflowDraft, workflowDraftInput, workflowDraftKey } from './workflowDraft'

const task = (id: string): WorkflowTaskDefinition => ({ id, title: id, description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], execution: 'required', review_policy: 'none', default_tags: [], artifacts: [] })
const definition: WorkflowDefinition = {
  id: 'release', version: 2, name: 'Release', description: '', agent_instructions: '', suggested_tags: [], status: 'published',
  graph: { start_task_ids: ['a'], tasks: [{ ...task('a'), artifacts: [{ name: 'commit', description: 'Provide the immutable commit.' }] }, task('b')], relations: [{ id: 'ab', from_task_id: 'a', to_task_id: 'b', label: 'Ready', agent_guidance: 'Continue when implementation is ready.' }], max_task_executions: 10 },
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
    draft.tasks[0].allowed_roles.push('backend')
    draft.tasks[0].artifacts[0].description = 'Changed locally'

    expect(draft.baseVersion).toBe(2)
    expect(draft.targetVersion).toBe(3)
    expect(definition.graph.tasks[0].allowed_roles).toEqual([])
    expect(definition.graph.tasks[0].artifacts[0].description).toBe('Provide the immutable commit.')
  })

  it('maintains tasks, starts, and relations as one graph operation', () => {
    let draft = newWorkflowDraft()
    draft = appendWorkflowTask(draft, task('a'))
    draft = appendWorkflowTask(draft, task('b'))
    draft = connectWorkflowTasks(draft, 'a', 'b', 'ab')
    draft = connectWorkflowTasks(draft, 'a', 'b', 'duplicate')

    expect(draft.startTaskIDs).toEqual(['a'])
    expect(draft.relations).toHaveLength(1)
    expect(draft.relations[0]).toMatchObject({ label: '', agent_guidance: '' })
    expect(toggleWorkflowStartTask(draft, 'b').startTaskIDs).toEqual(['a', 'b'])
    expect(deleteWorkflowTask(draft, 'a')).toMatchObject({ tasks: [{ id: 'b' }], relations: [], startTaskIDs: [] })
  })

  it('normalizes relation guidance missing from an older local draft', () => {
    const legacy = newWorkflowDraft()
    legacy.relations = [{ id: 'ab', from_task_id: 'a', to_task_id: 'b' }]
    localStorage.setItem(workflowDraftKey(null, null), JSON.stringify(legacy))

    expect(loadWorkflowDraft(workflowDraftKey(null, null))?.relations[0]).toMatchObject({ label: '', agent_guidance: '' })
  })

  it('preserves a self relation because the backend treats it as a workflow cycle', () => {
    let draft = appendWorkflowTask(newWorkflowDraft(), task('a'))
    draft.name = 'Retryable task'
    draft = connectWorkflowTasks(draft, 'a', 'a', 'retry')

    expect(validateWorkflowDraft(draft)).toEqual([])
    expect(workflowDraftInput(draft).graph.relations).toEqual([
      { id: 'retry', from_task_id: 'a', to_task_id: 'a', label: '', agent_guidance: '' },
    ])
  })

  it('validates simple client rules and maps the publish contract', () => {
    const empty = newWorkflowDraft()
    expect(validateWorkflowDraft(empty)).toEqual(['name', 'tasks', 'start'])

    const draft = draftFromDefinition(definition)
    const input = workflowDraftInput(draft)
    expect(validateWorkflowDraft(draft)).toEqual([])
    expect(input).toMatchObject({ id: 'release', version: 3, status: 'published', graph: { start_task_ids: ['a'], max_task_executions: 10 } })
    expect(input.graph.relations).toEqual([{ id: 'ab', from_task_id: 'a', to_task_id: 'b', label: 'Ready', agent_guidance: 'Continue when implementation is ready.' }])
    expect(input.graph.tasks[0].artifacts).toEqual([{ name: 'commit', description: 'Provide the immutable commit.' }])

    draft.tasks[0].artifacts.push({ name: 'commit', description: 'Duplicate' })
    expect(validateWorkflowDraft(draft)).toContain('duplicate-artifacts')
  })
})
