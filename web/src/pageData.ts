import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Identity } from './types'

export function useHomeData(identity: Identity, active: boolean) {
  const workItems = useQuery({ queryKey: ['work-items', identity], queryFn: () => api.listWorkItems(identity), enabled: active })
  const attention = useQuery({ queryKey: ['human-attention', identity], queryFn: () => api.listHumanAttention(identity), enabled: active })
  return { workItems, attention }
}

export function useWorkItemData(identity: Identity, workItemID: string | null) {
  return useQuery({
    queryKey: ['work-item', identity, workItemID],
    queryFn: () => api.getWorkItem(identity, workItemID!),
    enabled: Boolean(workItemID),
  })
}
