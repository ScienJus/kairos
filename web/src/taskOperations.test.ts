import { describe, expect, it, vi } from 'vitest'
import type { QueryClient } from '@tanstack/react-query'
import type { Claim, Identity, Task } from './types'
import { canAddBlackboardChild, canSkipBlackboardTask, refreshHomeState, refreshTaskState, refreshWorkItemState } from './taskOperations'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function task(status: Task['status']): Task {
  return {
    id: 'task-1', work_item_id: 'work-1', status: status, active_claim_id: null, parent_task_id: null,
    workflow_task_id: null, workflow_activation_id: null, decomposed_at: null,
    title: 'Task', description: '', acceptance_criteria: '', executor: 'human', allowed_roles: [], tags: [],
    reviews: [], submissions: [], failures: [], transition_decisions: [], position: 0,
    created_at: '', updated_at: '', completed_at: null, skipped_by: null, skip_reason: '',
    execution: null, review_policy: null, version: 0,
  }
}

const claim: Claim = {
  id: 'claim-1', task_id: 'task-1', executor: { kind: 'human', id: 'human-1' },
  claimed_at: '', last_heartbeat_at: '', lease_until: '', lease_seconds: 0, ended_at: null, end_reason: '',
}

describe('Blackboard operation visibility', () => {
  it('allows adding children only while the parent waits for children', () => {
    expect(canAddBlackboardChild(task('waiting_children'))).toBe(true)
    expect(canAddBlackboardChild(task('pending'))).toBe(false)
  })

  it('allows skipping only an unclaimed pending task', () => {
    expect(canSkipBlackboardTask(task('pending'), null)).toBe(true)
    expect(canSkipBlackboardTask(task('pending'), claim)).toBe(false)
    expect(canSkipBlackboardTask(task('working'), null)).toBe(false)
  })
})

describe('Task state refresh', () => {
  it('keeps refresh helpers inside their page boundary', async () => {
    const invalidateQueries = vi.fn(() => Promise.resolve())
    const queryClient = { invalidateQueries } as unknown as QueryClient

    await refreshHomeState(queryClient, identity)
    expect(invalidateQueries).toHaveBeenNthCalledWith(1, { queryKey: ['work-items', identity] })
    expect(invalidateQueries).toHaveBeenNthCalledWith(2, { queryKey: ['human-attention', identity] })

    invalidateQueries.mockClear()
    await refreshWorkItemState(queryClient, identity, 'work-1')
    expect(invalidateQueries).toHaveBeenCalledOnce()
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['work-item', identity, 'work-1'] })
  })

  it('waits only for the visible task and work item state', async () => {
    const releases: Array<() => void> = []
    const invalidateQueries = vi.fn(() => new Promise<void>(resolve => releases.push(resolve)))
    const refresh = refreshTaskState({ invalidateQueries } as unknown as QueryClient, identity, 'task-1', 'work-1')

    expect(invalidateQueries).toHaveBeenCalledTimes(3)
    let completed = false
    void refresh.then(() => { completed = true })
    await Promise.resolve()
    expect(completed).toBe(false)

    releases.forEach(release => release())
    await refresh
    expect(completed).toBe(true)
    expect(invalidateQueries).toHaveBeenCalledTimes(3)
    expect(invalidateQueries).toHaveBeenNthCalledWith(1, { queryKey: ['task-detail', identity, 'task-1'] })
    expect(invalidateQueries).toHaveBeenNthCalledWith(2, { queryKey: ['task-context', identity, 'task-1'] })
    expect(invalidateQueries).toHaveBeenNthCalledWith(3, { queryKey: ['work-item', identity, 'work-1'] })
  })
})
