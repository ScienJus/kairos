import { type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { api } from './api'
import { useI18n } from './i18n'
import { refreshHomeState } from './taskOperations'
import type { CreateWorkItemInput, Identity, Mode, WorkItem } from './types'
import { FormError, Modal, formValue, splitValues } from './ui'

type SelectableDefinition = Awaited<ReturnType<typeof api.listDefinitions>>[number]
export type WorkDefinitionTarget = { id: string; mode: Mode; name: string; version: number }

export function latestPublishedDefinitions(definitions: SelectableDefinition[]) {
  const latest = new Map<string, SelectableDefinition>()
  for (const definition of definitions) {
    const key = `${definition.mode}:${definition.id}`
    const current = latest.get(key)
    if (!current || definition.version > current.version) latest.set(key, definition)
  }
  return [...latest.values()].filter(definition => definition.status === 'published')
}

export function CreateWorkModal({ open, onOpenChange, identity, definition, onCreated }: {
  open: boolean; onOpenChange: (open: boolean) => void; identity: Identity
  definition?: WorkDefinitionTarget | null; onCreated?: (workItem: WorkItem) => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const definitions = useQuery({ queryKey: ['definitions', identity], queryFn: () => api.listDefinitions(identity), enabled: open && !definition, staleTime: 60_000 })
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

  const availableDefinitions = latestPublishedDefinitions(definitions.data ?? [])
  return <Modal open={open} onOpenChange={onOpenChange} title={t('openNewWork')} eyebrow={t('newWork')}>
    <form className="form-grid" onSubmit={submit}>
      {definition ? <div className="selected-definition wide"><span>{t('coordinationDefinition')}</span><strong>{definition.name}</strong><small>v{definition.version} · {t(definition.mode === 'workflow' ? 'workflow' : 'blackboard')}</small></div> : <label className="wide">{t('coordinationDefinition')}<select name="definition" required defaultValue=""><option value="" disabled>{t('selectDefinition')}</option>{availableDefinitions.map(item => <option key={`${item.mode}-${item.id}-${item.version}`} value={`${item.id}|${item.mode}`}>{item.name} · v{item.version} · {t(item.mode === 'workflow' ? 'workflow' : 'blackboard')}</option>)}</select></label>}
      <label className="wide">{t('title')}<input name="title" required /></label>
      <label className="wide">{t('goal')}<textarea name="goal" required rows={2} /></label>
      <label className="wide">{t('context')}<textarea name="context" rows={2} /></label>
      <label>{t('constraints')}<textarea name="constraints" rows={2} /></label>
      <label>{t('acceptanceCriteria')}<textarea name="acceptance" rows={2} /></label>
      {(!definition || definition.mode === 'blackboard') && <label className="wide">{t('acceptanceMode')}<select name="acceptance_mode" defaultValue="none"><option value="none">{t('acceptanceNone')}</option><option value="agent">{t('acceptanceAgent')}</option><option value="human">{t('acceptanceHuman')}</option></select></label>}
      <label className="wide">{t('tags')}<input name="tags" placeholder="development, backend" /></label>
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
