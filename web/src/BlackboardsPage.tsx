import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { ArrowLeft, ArrowRight, BookOpen, Plus, Tag, XCircle } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, APIError } from './api'
import type { WorkDefinitionTarget } from './AppModals'
import { useI18n } from './i18n'
import type { RouteState } from './route'
import type { CreateDefinitionInput, Definition, Identity } from './types'
import { FormError, formValue, splitValues } from './ui'

export function BlackboardsPage({ identity, blackboardID, blackboardVersion, navigate, onStartWork }: {
  identity: Identity
  blackboardID: string | null
  blackboardVersion: number | null
  navigate: (route: RouteState, replace?: boolean) => void
  onStartWork: (definition: WorkDefinitionTarget) => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const definitions = useQuery({ queryKey: ['blackboard-definitions', identity], queryFn: () => api.listBlackboardDefinitions(identity) })
  const [editing, setEditing] = useState(false)
  const versionsByID = useMemo(() => {
    const groups = new Map<string, Definition[]>()
    for (const definition of definitions.data ?? []) groups.set(definition.ID, [...(groups.get(definition.ID) ?? []), definition])
    for (const versions of groups.values()) versions.sort((left, right) => right.Version - left.Version)
    return [...groups.values()].sort((left, right) => left[0].Name.localeCompare(right[0].Name))
  }, [definitions.data])
  const selectedVersions = blackboardID ? versionsByID.find(versions => versions[0].ID === blackboardID) ?? [] : []
  const selected = selectedVersions.find(item => item.Version === blackboardVersion) ?? selectedVersions[0] ?? null
  const selectedIsLatest = selected !== null && selected.Version === selectedVersions[0]?.Version

  useEffect(() => {
    if (!blackboardID || !selected || blackboardVersion === selected.Version) return
    navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID, blackboardVersion: selected.Version }, true)
  }, [blackboardID, blackboardVersion, selected])

  const create = useMutation({
    mutationFn: (input: CreateDefinitionInput) => api.createDefinition(identity, input),
    onSuccess: async definition => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['blackboard-definitions', identity] }),
        queryClient.invalidateQueries({ queryKey: ['definitions', identity] }),
      ])
      setEditing(false)
      navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: definition.ID, blackboardVersion: definition.Version })
    },
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    create.mutate({
      id: selected?.ID ?? formValue(data, 'id'), version: selected ? selectedVersions[0].Version + 1 : 1,
      name: formValue(data, 'name'), description: formValue(data, 'description'),
      agent_instructions: formValue(data, 'instructions'), suggested_tags: splitValues(data.get('tags')), status: 'published',
    })
  }

  return <section className="blackboards-page">
    <header className="library-heading"><div><span>{t('blackboardLibrary')}</span><h1>{t('blackboardsTitle')}</h1><p>{t('blackboardsBody')}</p></div><button className="primary-button" onClick={() => { navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: null }); setEditing(true) }}><Plus size={16} />{t('newBlackboard')}</button></header>
    {definitions.error && <div className="error-banner"><XCircle size={16} /><span>{definitions.error instanceof APIError ? definitions.error.message : t('unreachable')}</span></div>}
    <div className="library-layout">
      <nav className="definition-shelf" aria-label={t('blackboardsTitle')}>
        {definitions.isLoading && <div className="library-empty">{t('loadingBlackboards')}</div>}
        {!definitions.isLoading && versionsByID.length === 0 && <div className="library-empty"><BookOpen size={22} /><p>{t('noBlackboards')}</p></div>}
        {versionsByID.map(versions => {
          const latest = versions[0]
          return <button key={latest.ID} className={latest.ID === blackboardID ? 'selected' : ''} onClick={() => { setEditing(false); navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: latest.ID, blackboardVersion: latest.Version }) }}><strong>{latest.Name}</strong><span>{latest.Description || t('noDescription')}</span><small>v{latest.Version} · {t(latest.Status === 'published' ? 'published' : latest.Status === 'draft' ? 'draft' : 'archived')}</small></button>
        })}
      </nav>

      <article className="definition-page">
        {editing && <DefinitionEditor source={selected} nextVersion={selected ? selectedVersions[0].Version + 1 : 1} pending={create.isPending} error={create.error} onSubmit={submit} onCancel={() => setEditing(false)} />}
        {!editing && selected && <>
          <div className="definition-title"><div><span>{selected.ID} · v{selected.Version}</span><h2>{selected.Name}</h2><p>{selected.Description || t('noDescription')}</p></div>{selectedIsLatest && selected.Status === 'published' && <div className="definition-actions"><button className="quiet-button" onClick={() => setEditing(true)}>{t('createNextVersion')}</button><button className="primary-button" onClick={() => onStartWork({ id: selected.ID, mode: 'blackboard', name: selected.Name, version: selected.Version })}>{t('useThisBlackboard')}<ArrowRight size={15} /></button></div>}</div>
          <DefinitionSection title={t('agentInstructions')} value={selected.AgentInstructions || t('noAgentInstructions')} />
          <section className="definition-section"><h3>{t('suggestedTags')}</h3>{selected.SuggestedTags.length > 0 ? <div className="definition-tags">{selected.SuggestedTags.map(tag => <span key={tag}><Tag size={12} />{tag}</span>)}</div> : <p>{t('noSuggestedTags')}</p>}</section>
          <section className="version-history"><h3>{t('versionHistory')}</h3>{selectedVersions.map(version => <button key={version.Version} className={version.Version === selected.Version ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: version.ID, blackboardVersion: version.Version })}><span>v{version.Version}</span><strong>{t(version.Status === 'published' ? 'published' : version.Status === 'draft' ? 'draft' : 'archived')}</strong></button>)}</section>
        </>}
        {!editing && !selected && <div className="definition-welcome"><BookOpen size={26} /><h2>{t('chooseBlackboard')}</h2><p>{t('chooseBlackboardBody')}</p></div>}
      </article>
    </div>
  </section>
}

function DefinitionSection({ title, value }: { title: string; value: string }) {
  return <section className="definition-section"><h3>{title}</h3><p className="instruction-copy">{value}</p></section>
}

function DefinitionEditor({ source, nextVersion, pending, error, onSubmit, onCancel }: {
  source: Definition | null; nextVersion: number; pending: boolean; error: Error | null
  onSubmit: (event: FormEvent<HTMLFormElement>) => void; onCancel: () => void
}) {
  const { t } = useI18n()
  return <form className="definition-editor" onSubmit={onSubmit} key={`${source?.ID ?? 'new'}-${nextVersion}`}>
    <button type="button" className="back-button" onClick={onCancel}><ArrowLeft size={16} />{t('cancelEditing')}</button>
    <div className="definition-editor-heading"><span>{source ? t('newVersion', { version: nextVersion }) : t('newBlackboard')}</span><h2>{source ? source.Name : t('untitledBlackboard')}</h2></div>
    {!source && <label>{t('definitionID')}<input name="id" required pattern="[a-z0-9-]+" placeholder="product-development" /></label>}
    <label>{t('displayName')}<input name="name" required defaultValue={source?.Name} /></label>
    <label>{t('description')}<textarea name="description" rows={3} defaultValue={source?.Description} /></label>
    <label>{t('agentInstructions')}<textarea name="instructions" rows={8} defaultValue={source?.AgentInstructions} /></label>
    <label>{t('suggestedTags')}<input name="tags" defaultValue={source?.SuggestedTags.join(', ')} placeholder="backend, product" /></label>
    {error && <FormError error={error} />}
    <div className="editor-actions"><button type="button" className="quiet-button" onClick={onCancel}>{t('cancel')}</button><button className="primary-button" disabled={pending}>{t(source ? 'publishNewVersion' : 'publishBlackboard')}</button></div>
  </form>
}
