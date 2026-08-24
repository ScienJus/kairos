import type { QueryClient } from '@tanstack/react-query'
import type { Claim, Identity, Task } from './types'

export function canAddBlackboardChild(task: Task) {
  return task.status === 'waiting_children'
}

export function canSkipBlackboardTask(task: Task, activeClaim: Claim | null) {
  return task.status === 'pending' && activeClaim === null
}

export async function refreshHomeState(queryClient: QueryClient, identity: Identity) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ['work-items', identity] }),
    queryClient.invalidateQueries({ queryKey: ['human-attention', identity] }),
  ])
}

export async function refreshWorkItemState(queryClient: QueryClient, identity: Identity, workItemID: string) {
  await queryClient.invalidateQueries({ queryKey: ['work-item', identity, workItemID] })
}

export async function refreshTaskState(queryClient: QueryClient, identity: Identity, taskID: string, workItemID: string) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ['task-detail', identity, taskID] }),
    queryClient.invalidateQueries({ queryKey: ['task-context', identity, taskID] }),
    queryClient.invalidateQueries({ queryKey: ['work-item', identity, workItemID] }),
  ])
}
