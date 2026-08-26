import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Identity } from './types'

const activeWorkItemStatuses = ['open', 'awaiting_agent_acceptance', 'awaiting_human_acceptance'] as const
const settledWorkItemStatuses = ['completed', 'cancelled', 'failed'] as const

export function useHomeData(identity: Identity, active: boolean) {
  const activeWorkItems = useInfiniteQuery({
    queryKey: ['work-items', identity, 'active'],
    queryFn: ({ pageParam }) => api.listWorkItems(identity, pageParam, { statuses: [...activeWorkItemStatuses] }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: active,
  })
  const settledWorkItems = useInfiniteQuery({
    queryKey: ['work-items', identity, 'settled'],
    queryFn: ({ pageParam }) => api.listWorkItems(identity, pageParam, { statuses: [...settledWorkItemStatuses] }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: active,
  })
  const attention = useInfiniteQuery({
    queryKey: ['human-attention', identity],
    queryFn: ({ pageParam }) => api.listHumanAttention(identity, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: active,
  })
  return { activeWorkItems, settledWorkItems, attention }
}

export function useWorkItemData(identity: Identity, workItemID: string | null) {
  return useQuery({
    queryKey: ['work-item', identity, workItemID],
    queryFn: () => api.getWorkItem(identity, workItemID!),
    enabled: Boolean(workItemID),
  })
}

export function useWorkflowDefinitionData(identity: Identity, definitionID: string | null, version: number | null) {
  return useQuery({
    queryKey: ['workflow-definition', identity, definitionID, version],
    queryFn: () => api.getWorkflowDefinition(identity, definitionID!, version!),
    enabled: Boolean(definitionID && version),
  })
}
