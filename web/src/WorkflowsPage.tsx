import { useEffect, useMemo, useState } from 'react'
import { ArrowRight, GitBranch, Plus, Tag, XCircle } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
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
  const definitions = useQuery({ queryKey: ['workflow-definitions', identity], queryFn: () => api.listWorkflowDefinitions(identity) })
  const [selectedTaskID, setSelectedTaskID] = useState<string | null>(null)
  const versionsByID = useMemo(() => {
    const groups = new Map<string, WorkflowDefinition[]>()
    for (const definition of definitions.data ?? []) groups.set(definition.ID, [...(groups.get(definition.ID) ?? []), definition])
    for (const versions of groups.values()) versions.sort((left, right) => right.Version - left.Version)
    return [...groups.values()].sort((left, right) => left[0].Name.localeCompare(right[0].Name))
  }, [definitions.data])
  const selectedVersions = workflowID ? versionsByID.find(versions => versions[0].ID === workflowID) ?? [] : []
  const selected = selectedVersions.find(item => item.Version === workflowVersion) ?? selectedVersions[0] ?? null
  const selectedIsLatest = selected !== null && selected.Version === selectedVersions[0]?.Version
  const selectedTask = selected?.Graph.Tasks.find(task => task.ID === selectedTaskID) ?? null

  useEffect(() => {
    setSelectedTaskID(null)
    if (!workflowID || !selected || workflowVersion === selected.Version) return
    navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion: selected.Version }, true)
  }, [workflowID, workflowVersion, selected])

  return <section className="blackboards-page workflows-page">
    <header className="library-heading"><div><span>{t('workflowLibrary')}</span><h1>{t('workflowsTitle')}</h1><p>{t('workflowsLibraryBody')}</p></div><button className="primary-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: null, workflowVersion: null, workflowEditing: true })}><Plus size={16} />{t('newWorkflow')}</button></header>
    {definitions.error && <div className="error-banner"><XCircle size={16} /><span>{definitions.error instanceof APIError ? definitions.error.message : t('unreachable')}</span></div>}
    <div className="library-layout">
      <nav className="definition-shelf" aria-label={t('workflowsTitle')}>
        {definitions.isLoading && <div className="library-empty">{t('loadingWorkflows')}</div>}
        {!definitions.isLoading && versionsByID.length === 0 && <div className="library-empty"><GitBranch size={22} /><p>{t('noWorkflows')}</p></div>}
        {versionsByID.map(versions => { const latest = versions[0]; return <button key={latest.ID} className={latest.ID === workflowID ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: latest.ID, workflowVersion: latest.Version })}><strong>{latest.Name}</strong><span>{latest.Description || t('noDescription')}</span><small>v{latest.Version} · {latest.Graph.Tasks.length} {t('tasks')}</small></button> })}
      </nav>

      <article className="definition-page workflow-definition-page">
        {selected && <>
          <div className="definition-title"><div><span>{selected.ID} · v{selected.Version}</span><h2>{selected.Name}</h2><p>{selected.Description || t('noDescription')}</p></div>{selectedIsLatest && selected.Status === 'published' && <div className="definition-actions"><button className="quiet-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: selected.ID, workflowVersion: selected.Version, workflowEditing: true })}>{t('createNextVersion')}</button><button className="primary-button" onClick={() => onStartWork({ id: selected.ID, mode: 'workflow', name: selected.Name, version: selected.Version })}>{t('useThisWorkflow')}<ArrowRight size={15} /></button></div>}</div>
          <section className="workflow-summary"><div><span>{t('tasks')}</span><strong>{selected.Graph.Tasks.length}</strong></div><div><span>{t('connections')}</span><strong>{selected.Graph.Relations.length}</strong></div><div><span>{t('executionLimit')}</span><strong>{selected.Graph.MaxTaskExecutions}</strong></div></section>
          <section className="definition-section"><h3>{t('agentInstructions')}</h3><p className="instruction-copy">{selected.AgentInstructions || t('noAgentInstructions')}</p></section>
          <section className="workflow-map-section"><div><h3>{t('workflowStructure')}</h3><p>{t('selectNodeBody')}</p></div><WorkflowDefinitionMap definition={selected} selectedTaskID={selectedTaskID} onSelect={setSelectedTaskID} /></section>
          {selectedTask && <WorkflowTaskDetails task={selectedTask} />}
          <section className="definition-section"><h3>{t('suggestedTags')}</h3>{selected.SuggestedTags.length > 0 ? <div className="definition-tags">{selected.SuggestedTags.map(tag => <span key={tag}><Tag size={12} />{tag}</span>)}</div> : <p>{t('noSuggestedTags')}</p>}</section>
          <section className="version-history"><h3>{t('versionHistory')}</h3>{selectedVersions.map(version => <button key={version.Version} className={version.Version === selected.Version ? 'selected' : ''} onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: version.ID, workflowVersion: version.Version })}><span>v{version.Version}</span><strong>{t(version.Status === 'published' ? 'published' : version.Status === 'draft' ? 'draft' : 'archived')}</strong></button>)}</section>
        </>}
        {!selected && <div className="definition-welcome"><GitBranch size={26} /><h2>{t('chooseWorkflow')}</h2><p>{t('chooseWorkflowBody')}</p></div>}
      </article>
    </div>
  </section>
}

function WorkflowTaskDetails({ task }: { task: WorkflowTaskDefinition }) {
  const { t } = useI18n()
  return <section className="workflow-task-definition"><span>{t('selectedTask')}</span><h3>{task.Title}</h3><p>{task.Description || t('noDescription')}</p><dl><div><dt>{t('executor')}</dt><dd>{t(task.Executor)}</dd></div><div><dt>{t('role')}</dt><dd>{task.AllowedRoles.join(', ') || t('unrestricted')}</dd></div><div><dt>{t('executionPolicy')}</dt><dd>{t(task.Execution === 'optional' ? 'optionalTask' : 'requiredTask')}</dd></div><div><dt>{t('reviewPolicy')}</dt><dd>{t(task.ReviewPolicy === 'required' ? 'reviewRequiredPolicy' : task.ReviewPolicy === 'executor_decides' ? 'reviewExecutorDecides' : 'reviewNone')}</dd></div></dl><div><strong>{t('acceptance')}</strong><p>{task.AcceptanceCriteria || '—'}</p></div>{task.Artifacts.length > 0 && <div><strong>{t('expectedArtifacts')}</strong>{task.Artifacts.map(artifact => <p key={artifact.Name}><b>{artifact.Name}</b> · {artifact.Description}</p>)}</div>}{task.DefaultTags.length > 0 && <div className="definition-tags">{task.DefaultTags.map(tag => <span key={tag}>{tag}</span>)}</div>}</section>
}
