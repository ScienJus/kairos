import { type FormEvent } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ShieldCheck } from 'lucide-react'
import { api } from './api'
import { useI18n } from './i18n'
import { refreshHomeState } from './taskOperations'
import type { CreateWorkItemInput, Identity, Mode, WorkItem } from './types'
import { FormError, Modal, formValue, splitValues } from './ui'

export type WorkDefinitionTarget = { id: string; mode: Mode; name: string; version: number }

export function CreateWorkModal({ open, onOpenChange, identity, definition, onCreated }: {
  open: boolean; onOpenChange: (open: boolean) => void; identity: Identity
  definition?: WorkDefinitionTarget | null; onCreated?: (workItem: WorkItem) => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const blackboards = useInfiniteQuery({
    queryKey: ['blackboard-definitions', identity], queryFn: ({ pageParam }) => api.listBlackboardDefinitions(identity, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: open && !definition, staleTime: 60_000,
  })
  const workflows = useInfiniteQuery({
    queryKey: ['workflow-definitions', identity], queryFn: ({ pageParam }) => api.listWorkflowDefinitions(identity, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: open && !definition, staleTime: 60_000,
  })
  const mutation = useMutation({ mutationFn: (input: CreateWorkItemInput) => api.createWorkItem(identity, input), onSuccess: async workItem => { await refreshHomeState(queryClient, identity); onOpenChange(false); onCreated?.(workItem) } })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const [id, mode] = definition ? [definition.id, definition.mode] : formValue(data, 'definition').split('|')
    mutation.mutate({
      definition_id: id, mode: mode as CreateWorkItemInput['mode'],
      title: formValue(data, 'title'), goal: formValue(data, 'goal'), context: formValue(data, 'context'),
      constraints: formValue(data, 'constraints'), acceptance_criteria: formValue(data, 'acceptance'), acceptance_mode: (formValue(data, 'acceptance_mode') || 'none') as CreateWorkItemInput['acceptance_mode'], tags: splitValues(data.get('tags')),
    })
  }

  const availableDefinitions = [
    ...(blackboards.data?.pages.flatMap(page => page.data).map(item => ({ ...item, mode: 'blackboard' as const })) ?? []),
    ...(workflows.data?.pages.flatMap(page => page.data).map(item => ({ ...item, mode: 'workflow' as const })) ?? []),
  ]
  const definitionsError = blackboards.error ?? workflows.error
  const hasMoreDefinitions = blackboards.hasNextPage || workflows.hasNextPage
  const loadingMoreDefinitions = blackboards.isFetchingNextPage || workflows.isFetchingNextPage

  async function loadMoreDefinitions() {
    await Promise.all([
      blackboards.hasNextPage ? blackboards.fetchNextPage() : Promise.resolve(),
      workflows.hasNextPage ? workflows.fetchNextPage() : Promise.resolve(),
    ])
  }

  return <Modal open={open} onOpenChange={onOpenChange} title={t('openNewWork')} eyebrow={t('newWork')}>
    <form className="form-grid" onSubmit={submit}>
      {definition ? <div className="selected-definition wide"><span>{t('coordinationDefinition')}</span><strong>{definition.name}</strong><small>v{definition.version} · {t(definition.mode === 'workflow' ? 'workflow' : 'blackboard')}</small></div> : <label className="wide">{t('coordinationDefinition')}<select name="definition" required defaultValue=""><option value="" disabled>{t('selectDefinition')}</option>{availableDefinitions.map(item => <option key={`${item.mode}-${item.id}-${item.version}`} value={`${item.id}|${item.mode}`}>{item.name} · v{item.version} · {t(item.mode === 'workflow' ? 'workflow' : 'blackboard')}</option>)}</select></label>}
      {!definition && hasMoreDefinitions && <button type="button" className="load-more-button wide" disabled={loadingMoreDefinitions} onClick={loadMoreDefinitions}><ArrowDown size={15} />{t(loadingMoreDefinitions ? 'loadingMore' : 'loadMore')}</button>}
      <label className="wide">{t('title')}<input name="title" required /></label>
      <label className="wide">{t('goal')}<textarea name="goal" required rows={2} /></label>
      <label className="wide">{t('context')}<textarea name="context" rows={2} /></label>
      <label>{t('constraints')}<textarea name="constraints" rows={2} /></label>
      <label>{t('acceptanceCriteria')}<textarea name="acceptance" rows={2} /></label>
      {(!definition || definition.mode === 'blackboard') && <label className="wide">{t('acceptanceMode')}<select name="acceptance_mode" defaultValue="none"><option value="none">{t('acceptanceNone')}</option><option value="agent">{t('acceptanceAgent')}</option><option value="human">{t('acceptanceHuman')}</option></select></label>}
      <label className="wide">{t('tags')}<input name="tags" placeholder="development, backend" /></label>
      {definitionsError && <FormError error={definitionsError} />}
      {mutation.error && <FormError error={mutation.error} />}
      <div className="form-actions"><button type="button" onClick={() => onOpenChange(false)}>{t('cancel')}</button><button className="primary-button" disabled={mutation.isPending || (!definition && availableDefinitions.length === 0)}>{t('openWork')}</button></div>
    </form>
  </Modal>
}

export function IdentityModal({ open, onOpenChange, identity, onSave }: { open: boolean; onOpenChange: (value: boolean) => void; identity: Identity; onSave: (value: Identity) => void }) {
  const { t } = useI18n()
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    onSave({ id: formValue(data, 'id'), kind: 'human', role: '' })
  }
  return <Modal open={open} onOpenChange={onOpenChange} title={t('transportIdentity')} eyebrow={t('trustedMode')}>
    <form className="form-grid" onSubmit={submit}>
      <label className="wide">{t('actorID')}<input name="id" required defaultValue={identity.id} /></label>
      <div className="identity-warning wide"><ShieldCheck size={17} /><p>{t('trustedWarning')}</p></div>
      <div className="form-actions"><button type="button" onClick={() => onOpenChange(false)}>{t('cancel')}</button><button className="primary-button">{t('applyIdentity')}</button></div>
    </form>
  </Modal>
}
