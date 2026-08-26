import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { ArrowDown, ArrowLeft, ArrowRight, BookOpen, Plus, Tag, XCircle } from 'lucide-react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
  const definitions = useInfiniteQuery({
    queryKey: ['blackboard-definitions', identity], queryFn: ({ pageParam }) => api.listBlackboardDefinitions(identity, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
  })
  const loadedDefinitions = useMemo(() => definitions.data?.pages.flatMap(page => page.data) ?? [], [definitions.data])
  const addressedDefinitions = useInfiniteQuery({
    queryKey: ['blackboard-definition-versions', identity, blackboardID],
    queryFn: ({ pageParam }) => api.listBlackboardDefinitionVersions(identity, blackboardID!, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: Boolean(blackboardID),
  })
  const requestedDefinition = useQuery({
    queryKey: ['blackboard-definition', identity, blackboardID, blackboardVersion],
    queryFn: () => api.getBlackboardDefinition(identity, blackboardID!, blackboardVersion!),
    enabled: Boolean(blackboardID && blackboardVersion),
  })
  const addressedVersions = useMemo(() => {
    const versions = addressedDefinitions.data?.pages.flatMap(page => page.data) ?? []
    const byVersion = new Map(versions.map(definition => [definition.version, definition]))
    if (requestedDefinition.data) byVersion.set(requestedDefinition.data.version, requestedDefinition.data)
    return [...byVersion.values()].sort((left, right) => right.version - left.version)
  }, [addressedDefinitions.data, requestedDefinition.data])
  const [editing, setEditing] = useState(false)
  const versionsByID = useMemo(() => {
    const groups = new Map<string, Definition[]>()
    for (const definition of [...loadedDefinitions, ...addressedVersions]) {
      const versions = groups.get(definition.id) ?? []
      if (!versions.some(version => version.version === definition.version)) groups.set(definition.id, [...versions, definition])
    }
    for (const versions of groups.values()) versions.sort((left, right) => right.version - left.version)
    return [...groups.values()].sort((left, right) => left[0].name.localeCompare(right[0].name))
  }, [loadedDefinitions, addressedVersions])
  const selected = blackboardVersion ? requestedDefinition.data ?? null : addressedVersions[0] ?? null
  const selectedIsLatest = selected !== null && selected.version === addressedVersions[0]?.version
  const selectionLoading = Boolean(blackboardID && (addressedDefinitions.isLoading || (blackboardVersion && requestedDefinition.isLoading)))
  const selectionError = requestedDefinition.error ?? addressedDefinitions.error
  const selectionMissing = Boolean(blackboardID && !selectionLoading && !selectionError && !selected)

  useEffect(() => {
    if (!blackboardID || !selected || blackboardVersion) return
    navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID, blackboardVersion: selected.version }, true)
  }, [blackboardID, blackboardVersion, selected, navigate])

  const create = useMutation({
    mutationFn: (input: CreateDefinitionInput) => api.createDefinition(identity, input),
    onSuccess: async definition => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['blackboard-definitions', identity] }),
        queryClient.invalidateQueries({ queryKey: ['blackboard-definition-versions', identity, definition.id] }),
      ])
      setEditing(false)
      navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: definition.id, blackboardVersion: definition.version })
    },
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    create.mutate({
      id: selected?.id ?? formValue(data, 'id'), base_version: selected?.version,
      name: formValue(data, 'name'), description: formValue(data, 'description'),
      agent_instructions: formValue(data, 'instructions'), suggested_tags: splitValues(data.get('tags')),
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
          return <button key={latest.id} className={latest.id === blackboardID ? 'selected' : ''} onClick={() => { setEditing(false); navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: latest.id, blackboardVersion: latest.version }) }}><strong>{latest.name}</strong><span>{latest.description || t('noDescription')}</span><small>v{latest.version}</small></button>
        })}
        {definitions.hasNextPage && <button className="load-more-button" disabled={definitions.isFetchingNextPage} onClick={() => definitions.fetchNextPage()}><ArrowDown size={15} />{t(definitions.isFetchingNextPage ? 'loadingMore' : 'loadMore')}</button>}
      </nav>

      <article className="definition-page">
        {editing && <DefinitionEditor source={selected} nextVersion={selected ? addressedVersions[0].version + 1 : 1} pending={create.isPending} error={create.error} onSubmit={submit} onCancel={() => setEditing(false)} />}
        {!editing && selectionLoading && <div className="definition-welcome">{t('loadingBlackboards')}</div>}
        {!editing && (selectionError || selectionMissing) && <div className="definition-welcome"><XCircle size={26} /><h2>{t(selectionMissing ? 'definitionNotFound' : 'definitionUnavailable')}</h2>{selectionError && <p>{selectionError instanceof APIError ? selectionError.message : t('unreachable')}</p>}</div>}
        {!editing && !selectionLoading && !selectionError && selected && <>
          <div className="definition-title"><div><span>{selected.id} · v{selected.version}</span><h2>{selected.name}</h2><p>{selected.description || t('noDescription')}</p></div>{selectedIsLatest && <div className="definition-actions"><button className="quiet-button" onClick={() => setEditing(true)}>{t('createNextVersion')}</button><button className="primary-button" onClick={() => onStartWork({ id: selected.id, mode: 'blackboard', name: selected.name, version: selected.version })}>{t('useThisBlackboard')}<ArrowRight size={15} /></button></div>}</div>
          <DefinitionSection title={t('agentInstructions')} value={selected.agent_instructions || t('noAgentInstructions')} />
          <section className="definition-section"><h3>{t('suggestedTags')}</h3>{selected.suggested_tags.length > 0 ? <div className="definition-tags">{selected.suggested_tags.map(tag => <span key={tag}><Tag size={12} />{tag}</span>)}</div> : <p>{t('noSuggestedTags')}</p>}</section>
          <section className="version-history"><h3>{t('versionHistory')}</h3>{addressedVersions.map(version => <button key={version.version} className={version.version === selected.version ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', blackboardID: version.id, blackboardVersion: version.version })}><span>v{version.version}</span></button>)}{addressedDefinitions.hasNextPage && <button className="load-more-button" disabled={addressedDefinitions.isFetchingNextPage} onClick={() => addressedDefinitions.fetchNextPage()}><ArrowDown size={15} />{t(addressedDefinitions.isFetchingNextPage ? 'loadingMore' : 'loadMore')}</button>}</section>
        </>}
        {!editing && !blackboardID && <div className="definition-welcome"><BookOpen size={26} /><h2>{t('chooseBlackboard')}</h2><p>{t('chooseBlackboardBody')}</p></div>}
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
  return <form className="definition-editor" onSubmit={onSubmit} key={`${source?.id ?? 'new'}-${nextVersion}`}>
    <button type="button" className="back-button" onClick={onCancel}><ArrowLeft size={16} />{t('cancelEditing')}</button>
    <div className="definition-editor-heading"><span>{source ? t('newVersion', { version: nextVersion }) : t('newBlackboard')}</span><h2>{source ? source.name : t('untitledBlackboard')}</h2></div>
    {!source && <label>{t('definitionID')}<input name="id" required pattern="[a-z0-9-]+" placeholder="product-development" /></label>}
    <label>{t('displayName')}<input name="name" required defaultValue={source?.name} /></label>
    <label>{t('description')}<textarea name="description" rows={3} defaultValue={source?.description} /></label>
    <label>{t('agentInstructions')}<textarea name="instructions" rows={8} defaultValue={source?.agent_instructions} /></label>
    <label>{t('suggestedTags')}<input name="tags" defaultValue={source?.suggested_tags.join(', ')} placeholder="backend, product" /></label>
    {error && <FormError error={error} />}
    <div className="editor-actions"><button type="button" className="quiet-button" onClick={onCancel}>{t('cancel')}</button><button className="primary-button" disabled={pending}>{t(source ? 'publishNewVersion' : 'publishBlackboard')}</button></div>
  </form>
}
