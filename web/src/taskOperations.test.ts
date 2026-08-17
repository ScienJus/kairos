import { describe, expect, it, vi } from 'vitest'
import type { QueryClient } from '@tanstack/react-query'
import type { Claim, Identity, Task } from './types'
import { canAddBlackboardChild, canSkipBlackboardTask, refreshHomeState, refreshTaskState, refreshWorkItemState } from './taskOperations'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function task(status: Task['Status']): Task {
  return {
    ID: 'task-1', WorkItemID: 'work-1', Status: status, ActiveClaimID: null, ParentTaskID: null,
    Title: 'Task', Description: '', AcceptanceCriteria: '', Executor: 'human', AllowedRoles: [], Tags: [],
    Reviews: [], Submissions: [], Failures: [], TransitionDecisions: [], Position: 0,
    CreatedAt: '', UpdatedAt: '', CompletedAt: null,
  }
}

const claim: Claim = {
  ID: 'claim-1', TaskID: 'task-1', Executor: { Kind: 'human', ID: 'human-1' },
  ClaimedAt: '', EndedAt: null, EndReason: '',
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

    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    let completed = false
    void refresh.then(() => { completed = true })
    await Promise.resolve()
    expect(completed).toBe(false)

    releases.forEach(release => release())
    await refresh
    expect(completed).toBe(true)
    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    expect(invalidateQueries).toHaveBeenNthCalledWith(1, { queryKey: ['task-context', identity, 'task-1'] })
    expect(invalidateQueries).toHaveBeenNthCalledWith(2, { queryKey: ['work-item', identity, 'work-1'] })
  })
})
