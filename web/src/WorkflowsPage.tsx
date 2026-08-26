import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowRight, GitBranch, Plus, Tag, XCircle } from 'lucide-react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api, APIError } from './api'
import type { WorkDefinitionTarget } from './AppModals'
import { useI18n } from './i18n'
import type { RouteState } from './route'
import type { Identity, WorkflowDefinition, WorkflowTaskDefinition } from './types'
import { WorkflowDefinitionMap } from './WorkflowDefinitionMap'

export function WorkflowsPage({ identity, workflowID, workflowVersion, navigate, onStartWork }: {
  identity: Identity; workflowID: string | null; workflowVersion: number | null
  navigate: (route: RouteState, replace?: boolean) => void
  onStartWork: (definition: WorkDefinitionTarget) => void
}) {
  const { t } = useI18n()
  const definitions = useInfiniteQuery({
    queryKey: ['workflow-definitions', identity], queryFn: ({ pageParam }) => api.listWorkflowDefinitions(identity, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
  })
  const loadedDefinitions = useMemo(() => definitions.data?.pages.flatMap(page => page.data) ?? [], [definitions.data])
  const addressedDefinitions = useInfiniteQuery({
    queryKey: ['workflow-definition-versions', identity, workflowID],
    queryFn: ({ pageParam }) => api.listWorkflowDefinitionVersions(identity, workflowID!, pageParam),
    initialPageParam: undefined as string | undefined, getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: Boolean(workflowID),
  })
  const requestedDefinition = useQuery({
    queryKey: ['workflow-definition', identity, workflowID, workflowVersion],
    queryFn: () => api.getWorkflowDefinition(identity, workflowID!, workflowVersion!),
    enabled: Boolean(workflowID && workflowVersion),
  })
  const addressedVersions = useMemo(() => {
    const versions = addressedDefinitions.data?.pages.flatMap(page => page.data) ?? []
    const byVersion = new Map(versions.map(definition => [definition.version, definition]))
    if (requestedDefinition.data) byVersion.set(requestedDefinition.data.version, requestedDefinition.data)
    return [...byVersion.values()].sort((left, right) => right.version - left.version)
  }, [addressedDefinitions.data, requestedDefinition.data])
  const [selectedTaskID, setSelectedTaskID] = useState<string | null>(null)
  const versionsByID = useMemo(() => {
    const groups = new Map<string, WorkflowDefinition[]>()
    for (const definition of [...loadedDefinitions, ...addressedVersions]) {
      const versions = groups.get(definition.id) ?? []
      if (!versions.some(version => version.version === definition.version)) groups.set(definition.id, [...versions, definition])
    }
    for (const versions of groups.values()) versions.sort((left, right) => right.version - left.version)
    return [...groups.values()].sort((left, right) => left[0].name.localeCompare(right[0].name))
  }, [loadedDefinitions, addressedVersions])
  const selected = workflowVersion ? requestedDefinition.data ?? null : addressedVersions[0] ?? null
  const selectedIsLatest = selected !== null && selected.version === addressedVersions[0]?.version
  const selectionLoading = Boolean(workflowID && (addressedDefinitions.isLoading || (workflowVersion && requestedDefinition.isLoading)))
  const selectionError = requestedDefinition.error ?? addressedDefinitions.error
  const selectionMissing = Boolean(workflowID && !selectionLoading && !selectionError && !selected)
  const selectedTask = selected?.graph.tasks.find(task => task.id === selectedTaskID) ?? null

  useEffect(() => {
    setSelectedTaskID(null)
    if (!workflowID || !selected || workflowVersion) return
    navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion: selected.version }, true)
  }, [workflowID, workflowVersion, selected, navigate])

  return <section className="blackboards-page workflows-page">
    <header className="library-heading"><div><span>{t('workflowLibrary')}</span><h1>{t('workflowsTitle')}</h1><p>{t('workflowsLibraryBody')}</p></div><button className="primary-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: null, workflowVersion: null, workflowEditing: true })}><Plus size={16} />{t('newWorkflow')}</button></header>
    {definitions.error && <div className="error-banner"><XCircle size={16} /><span>{definitions.error instanceof APIError ? definitions.error.message : t('unreachable')}</span></div>}
    <div className="library-layout">
      <nav className="definition-shelf" aria-label={t('workflowsTitle')}>
        {definitions.isLoading && <div className="library-empty">{t('loadingWorkflows')}</div>}
        {!definitions.isLoading && versionsByID.length === 0 && <div className="library-empty"><GitBranch size={22} /><p>{t('noWorkflows')}</p></div>}
        {versionsByID.map(versions => { const latest = versions[0]; return <button key={latest.id} className={latest.id === workflowID ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: latest.id, workflowVersion: latest.version })}><strong>{latest.name}</strong><span>{latest.description || t('noDescription')}</span><small>v{latest.version} · {latest.graph.tasks.length} {t('tasks')}</small></button> })}
        {definitions.hasNextPage && <button className="load-more-button" disabled={definitions.isFetchingNextPage} onClick={() => definitions.fetchNextPage()}><ArrowDown size={15} />{t(definitions.isFetchingNextPage ? 'loadingMore' : 'loadMore')}</button>}
      </nav>

      <article className="definition-page workflow-definition-page">
        {selectionLoading && <div className="definition-welcome">{t('loadingWorkflows')}</div>}
        {(selectionError || selectionMissing) && <div className="definition-welcome"><XCircle size={26} /><h2>{t(selectionMissing ? 'definitionNotFound' : 'definitionUnavailable')}</h2>{selectionError && <p>{selectionError instanceof APIError ? selectionError.message : t('unreachable')}</p>}</div>}
        {!selectionLoading && !selectionError && selected && <>
          <div className="definition-title"><div><span>{selected.id} · v{selected.version}</span><h2>{selected.name}</h2><p>{selected.description || t('noDescription')}</p></div>{selectedIsLatest && <div className="definition-actions"><button className="quiet-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: selected.id, workflowVersion: selected.version, workflowEditing: true })}>{t('createNextVersion')}</button><button className="primary-button" onClick={() => onStartWork({ id: selected.id, mode: 'workflow', name: selected.name, version: selected.version })}>{t('useThisWorkflow')}<ArrowRight size={15} /></button></div>}</div>
          <section className="workflow-summary"><div><span>{t('tasks')}</span><strong>{selected.graph.tasks.length}</strong></div><div><span>{t('connections')}</span><strong>{selected.graph.relations.length}</strong></div><div><span>{t('executionLimit')}</span><strong>{selected.graph.max_task_executions}</strong></div></section>
          <section className="definition-section"><h3>{t('agentInstructions')}</h3><p className="instruction-copy">{selected.agent_instructions || t('noAgentInstructions')}</p></section>
          <section className="workflow-map-section"><div><h3>{t('workflowStructure')}</h3><p>{t('selectNodeBody')}</p></div><WorkflowDefinitionMap definition={selected} selectedTaskID={selectedTaskID} onSelect={setSelectedTaskID} /></section>
          {selectedTask && <WorkflowTaskDetails task={selectedTask} />}
          <section className="definition-section"><h3>{t('suggestedTags')}</h3>{selected.suggested_tags.length > 0 ? <div className="definition-tags">{selected.suggested_tags.map(tag => <span key={tag}><Tag size={12} />{tag}</span>)}</div> : <p>{t('noSuggestedTags')}</p>}</section>
          <section className="version-history"><h3>{t('versionHistory')}</h3>{addressedVersions.map(version => <button key={version.version} className={version.version === selected.version ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: version.id, workflowVersion: version.version })}><span>v{version.version}</span></button>)}{addressedDefinitions.hasNextPage && <button className="load-more-button" disabled={addressedDefinitions.isFetchingNextPage} onClick={() => addressedDefinitions.fetchNextPage()}><ArrowDown size={15} />{t(addressedDefinitions.isFetchingNextPage ? 'loadingMore' : 'loadMore')}</button>}</section>
        </>}
        {!workflowID && <div className="definition-welcome"><GitBranch size={26} /><h2>{t('chooseWorkflow')}</h2><p>{t('chooseWorkflowBody')}</p></div>}
      </article>
    </div>
  </section>
}

function WorkflowTaskDetails({ task }: { task: WorkflowTaskDefinition }) {
  const { t } = useI18n()
  return <section className="workflow-task-definition"><span>{t('selectedTask')}</span><h3>{task.title}</h3><p>{task.description || t('noDescription')}</p><dl><div><dt>{t('executor')}</dt><dd>{t(task.executor)}</dd></div><div><dt>{t('role')}</dt><dd>{task.allowed_roles.join(', ') || t('unrestricted')}</dd></div><div><dt>{t('executionPolicy')}</dt><dd>{t(task.execution === 'optional' ? 'optionalTask' : 'requiredTask')}</dd></div><div><dt>{t('reviewPolicy')}</dt><dd>{t(task.review_policy === 'required' ? 'reviewRequiredPolicy' : task.review_policy === 'executor_decides' ? 'reviewExecutorDecides' : 'reviewNone')}</dd></div></dl><div><strong>{t('acceptance')}</strong><p>{task.acceptance_criteria || '—'}</p></div>{task.artifacts.length > 0 && <div><strong>{t('expectedArtifacts')}</strong>{task.artifacts.map(artifact => <p key={artifact.name}><b>{artifact.name}</b> · {artifact.description}</p>)}</div>}{task.default_tags.length > 0 && <div className="definition-tags">{task.default_tags.map(tag => <span key={tag}>{tag}</span>)}</div>}</section>
}
