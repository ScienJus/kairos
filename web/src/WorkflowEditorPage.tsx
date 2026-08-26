import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ArrowLeft, Check, Plus, Trash2, XCircle } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, APIError } from './api'
import { useI18n } from './i18n'
import type { RouteState } from './route'
import type { Identity, WorkflowRelationDefinition, WorkflowTaskDefinition } from './types'
import { FormError } from './ui'
import { WorkflowEditorMap, type WorkflowTaskPlacement } from './WorkflowEditorMap'
import { appendWorkflowTask, connectWorkflowTasks, deleteWorkflowTask, draftFromDefinition, loadWorkflowDraft, newWorkflowDraft, removeWorkflowDraft, saveWorkflowDraft, toggleWorkflowStartTask, validateWorkflowDraft, workflowDraftInput, workflowDraftKey, type WorkflowDraft } from './workflowDraft'

export function WorkflowEditorPage({ identity, workflowID, workflowVersion, navigate }: {
  identity: Identity; workflowID: string | null; workflowVersion: number | null
  navigate: (route: RouteState, replace?: boolean) => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const definition = useQuery({
    queryKey: ['workflow-definition', identity, workflowID, workflowVersion],
    queryFn: () => api.getWorkflowDefinition(identity, workflowID!, workflowVersion!),
    enabled: Boolean(workflowID && workflowVersion),
  })
  const latestDefinition = useQuery({
	queryKey: ['workflow-definition-latest', identity, workflowID],
	queryFn: () => api.getLatestWorkflowDefinition(identity, workflowID!),
    enabled: Boolean(workflowID && workflowVersion),
  })
  const base = definition.data ?? null
  const latest = latestDefinition.data ?? null
  const storageKey = workflowDraftKey(workflowID, workflowVersion)
  const [draft, setDraft] = useState<WorkflowDraft | null>(() => workflowID ? null : loadWorkflowDraft(storageKey) ?? newWorkflowDraft())
  const [selectedTaskID, setSelectedTaskID] = useState<string | null>(null)
  const [selectedRelationID, setSelectedRelationID] = useState<string | null>(null)
  const [newTaskTitle, setNewTaskTitle] = useState('')
  const [addingTask, setAddingTask] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const [validationErrors, setValidationErrors] = useState<string[]>([])
  const [saved, setSaved] = useState(true)
  const [mobileTab, setMobileTab] = useState<'canvas' | 'properties'>('canvas')
  const [newTaskPlacement, setNewTaskPlacement] = useState<WorkflowTaskPlacement | null>(null)
  const [automaticallySelectedTaskID, setAutomaticallySelectedTaskID] = useState<string | null>(null)

  useEffect(() => {
    if (!workflowID || !base || !latest || base.version !== latest.version || draft) return
    setDraft(loadWorkflowDraft(storageKey) ?? draftFromDefinition(base))
  }, [workflowID, base, latest, draft, storageKey])

  useEffect(() => {
    if (!draft) return
    setSaved(false)
    const timer = window.setTimeout(() => { saveWorkflowDraft(storageKey, draft); setSaved(true) }, 250)
    return () => window.clearTimeout(timer)
  }, [draft, storageKey])

  const selectedTask = draft?.tasks.find(task => task.id === selectedTaskID) ?? null
  const selectedRelation = draft?.relations.find(relation => relation.id === selectedRelationID) ?? null
  const updateDraft = (update: (current: WorkflowDraft) => WorkflowDraft) => setDraft(current => current ? update(current) : current)
  const updateTask = (update: Partial<WorkflowTaskDefinition>) => updateDraft(current => ({ ...current, tasks: current.tasks.map(task => task.id === selectedTaskID ? { ...task, ...update } : task) }))
  const updateRelation = (update: Partial<WorkflowRelationDefinition>) => updateDraft(current => ({ ...current, relations: current.relations.map(relation => relation.id === selectedRelationID ? { ...relation, ...update } : relation) }))
  const handleTaskPlaced = useCallback((taskID: string) => setNewTaskPlacement(current => current?.taskID === taskID ? null : current), [])

  const publish = useMutation({
    mutationFn: (current: WorkflowDraft) => api.createWorkflowDefinition(identity, workflowDraftInput(current)),
    onSuccess: async definition => {
      removeWorkflowDraft(storageKey)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['workflow-definitions', identity] }),
        queryClient.invalidateQueries({ queryKey: ['workflow-definition-versions', identity, definition.id] }),
        queryClient.invalidateQueries({ queryKey: ['workflow-definition-latest', identity, definition.id] }),
      ])
      navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID: definition.id, workflowVersion: definition.version })
    },
  })

  function addTask(event: FormEvent) {
    event.preventDefault()
    if (!newTaskTitle.trim()) return
    const task: WorkflowTaskDefinition = { id: crypto.randomUUID(), title: newTaskTitle.trim(), description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], execution: 'required', review_policy: 'none', default_tags: [], artifacts: [] }
    setNewTaskPlacement({ taskID: task.id, anchorTaskID: selectedTaskID === automaticallySelectedTaskID ? null : selectedTaskID })
    updateDraft(current => appendWorkflowTask(current, task))
    setAutomaticallySelectedTaskID(task.id)
    setSelectedTaskID(task.id); setSelectedRelationID(null); setNewTaskTitle(''); setAddingTask(false); setMobileTab('properties')
  }

  function connect(from: string, to: string) {
    updateDraft(current => connectWorkflowTasks(current, from, to, crypto.randomUUID()))
  }

  function deleteTask() {
    if (!selectedTaskID) return
    updateDraft(current => deleteWorkflowTask(current, selectedTaskID))
    setSelectedTaskID(null); setAutomaticallySelectedTaskID(null); setConfirmDelete(false)
  }

  function tryPublish() {
    if (!draft) return
    const errors = validateWorkflowDraft(draft)
    setValidationErrors(errors)
    if (errors.length === 0) publish.mutate(draft)
  }

  function discard() {
    removeWorkflowDraft(storageKey)
    navigate(workflowID && workflowVersion ? { workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion } : { workItemID: null, taskID: null, homeView: 'all', workflowID: null })
  }

  if (workflowID && (definition.isLoading || latestDefinition.isLoading)) return <div className="editor-loading">{t('loadingWorkflows')}</div>
  const loadError = definition.error ?? latestDefinition.error
  if (workflowID && loadError) return <div className="editor-loading"><XCircle size={20} /><strong>{loadError instanceof APIError ? loadError.message : t('unreachable')}</strong><button className="quiet-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion })}>{t('back')}</button></div>
  if (workflowID && base && (!latest || base.version !== latest.version)) return <div className="editor-loading"><XCircle size={20} /><strong>{t('workflowEditLatestOnly')}</strong><button className="quiet-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion: latest?.version ?? workflowVersion })}>{t('back')}</button></div>
  if (!draft) return null

  return <section className="workflow-editor-page">
    <header className="workflow-editor-header"><button className="back-button" onClick={() => navigate(workflowID && workflowVersion ? { workItemID: null, taskID: null, homeView: 'all', workflowID, workflowVersion } : { workItemID: null, taskID: null, homeView: 'all', workflowID: null })}><ArrowLeft size={16} />{t('leaveEditor')}</button><div><span>{workflowID ? t('newVersion', { version: draft.targetVersion }) : t('newWorkflow')}</span><h1>{draft.name || t('untitledWorkflow')}</h1></div><div className="editor-publish"><small><Check size={13} />{t(saved ? 'savedLocally' : 'savingLocally')}</small><button className="quiet-button editor-discard-trigger" title={t('discardDraft')} aria-label={t('discardDraft')} onClick={() => setConfirmDiscard(true)}><Trash2 size={14} /><span>{t('discardDraft')}</span></button><button className="primary-button" disabled={publish.isPending} onClick={tryPublish}>{t('publishVersion', { version: draft.targetVersion })}</button></div></header>
    {(validationErrors.length > 0 || publish.error) && <div className="editor-error-summary"><XCircle size={17} /><div>{validationErrors.length > 0 && <><p>{t('workflowValidationFailed', { count: validationErrors.length })}</p><ul>{validationErrors.map(error => <li key={error}>{t(validationMessage(error))}</li>)}</ul></>}{publish.error && <FormError error={publish.error} />}</div></div>}
    {confirmDiscard && <div className="inline-confirm editor-discard"><p>{t('discardDraftBody')}</p><button onClick={() => setConfirmDiscard(false)}>{t('keepEditing')}</button><button className="danger-button" onClick={discard}>{t('confirmDiscard')}</button></div>}
    <div className="editor-mobile-tabs"><button className={mobileTab === 'canvas' ? 'active' : ''} onClick={() => setMobileTab('canvas')}>{t('canvas')}</button><button className={mobileTab === 'properties' ? 'active' : ''} onClick={() => setMobileTab('properties')}>{t('properties')}</button></div>
    <div className="workflow-editor-layout">
      <div className={`editor-canvas ${mobileTab === 'canvas' ? 'mobile-active' : ''}`}><div className="editor-toolbar"><div>{addingTask ? <form onSubmit={addTask}><input autoFocus value={newTaskTitle} onChange={event => setNewTaskTitle(event.target.value)} placeholder={t('taskTitle')} /><button className="primary-button" disabled={!newTaskTitle.trim()}>{t('add')}</button><button type="button" onClick={() => { setAddingTask(false); setNewTaskTitle('') }}>{t('cancel')}</button></form> : <button className="text-button" onClick={() => setAddingTask(true)}><Plus size={15} />{t('addTask')}</button>}</div>{selectedRelationID && <button className="quiet-button danger-button" onClick={() => { updateDraft(current => ({ ...current, relations: current.relations.filter(relation => relation.id !== selectedRelationID) })); setSelectedRelationID(null) }}><Trash2 size={14} />{t('deleteConnection')}</button>}</div><WorkflowEditorMap draft={draft} selectedTaskID={selectedTaskID} selectedRelationID={selectedRelationID} newTaskPlacement={newTaskPlacement} onTaskPlaced={handleTaskPlaced} onSelectTask={id => { setSelectedTaskID(id || null); setAutomaticallySelectedTaskID(null); setSelectedRelationID(null); setConfirmDelete(false) }} onSelectRelation={id => { setSelectedRelationID(id); setAutomaticallySelectedTaskID(null); if (id) setSelectedTaskID(null) }} onConnect={connect} onDeleteRelation={id => updateDraft(current => ({ ...current, relations: current.relations.filter(relation => relation.id !== id) }))} /></div>
      <aside className={`editor-properties ${mobileTab === 'properties' ? 'mobile-active' : ''}`}>{selectedRelation ? <RelationProperties relation={selectedRelation} draft={draft} onChange={updateRelation} /> : selectedTask ? <TaskProperties task={selectedTask} isStart={draft.startTaskIDs.includes(selectedTask.id)} onChange={updateTask} onToggleStart={() => updateDraft(current => toggleWorkflowStartTask(current, selectedTask.id))} confirmDelete={confirmDelete} onRequestDelete={() => setConfirmDelete(true)} onCancelDelete={() => setConfirmDelete(false)} onDelete={deleteTask} /> : <WorkflowProperties draft={draft} onChange={update => updateDraft(current => ({ ...current, ...update }))} />}</aside>
    </div>
  </section>
}

function RelationProperties({ relation, draft, onChange }: { relation: WorkflowRelationDefinition; draft: WorkflowDraft; onChange: (update: Partial<WorkflowRelationDefinition>) => void }) {
  const { t } = useI18n()
  const from = draft.tasks.find(task => task.id === relation.from_task_id)?.title ?? relation.from_task_id
  const to = draft.tasks.find(task => task.id === relation.to_task_id)?.title ?? relation.to_task_id
  return <div className="property-form relation-properties"><span>{t('relationSettings')}</span><h2>{from} <i aria-hidden="true">→</i> {to}</h2><small className="internal-id">{relation.id}</small><p className="property-intro">{t('relationGuidanceBody')}</p><label>{t('relationLabel')}<input value={relation.label ?? ''} onChange={event => onChange({ label: event.target.value })} placeholder={t('relationLabelPlaceholder')} /><small>{t('relationLabelHint')}</small></label><label>{t('relationAgentGuidance')}<textarea rows={7} value={relation.agent_guidance ?? ''} onChange={event => onChange({ agent_guidance: event.target.value })} placeholder={t('relationGuidancePlaceholder')} /><small>{t('relationGuidanceHint')}</small></label></div>
}

function WorkflowProperties({ draft, onChange }: { draft: WorkflowDraft; onChange: (update: Partial<WorkflowDraft>) => void }) {
  const { t } = useI18n()
  return <div className="property-form"><span>{t('workflowSettings')}</span><h2>{t('workflowDetails')}</h2><label>{t('displayName')}<input value={draft.name} onChange={event => onChange({ name: event.target.value })} /></label><label>{t('description')}<textarea rows={3} value={draft.description} onChange={event => onChange({ description: event.target.value })} /></label><label>{t('agentInstructions')}<textarea rows={6} value={draft.agentInstructions} onChange={event => onChange({ agentInstructions: event.target.value })} /></label><label>{t('suggestedTags')}<CommaValuesInput values={draft.suggestedTags} onChange={suggestedTags => onChange({ suggestedTags })} /></label><label>{t('executionLimit')}<input type="number" min={1} value={draft.maxTaskExecutions} onChange={event => onChange({ maxTaskExecutions: Number(event.target.value) })} /></label></div>
}

function TaskProperties({ task, isStart, onChange, onToggleStart, confirmDelete, onRequestDelete, onCancelDelete, onDelete }: {
  task: WorkflowTaskDefinition; isStart: boolean; onChange: (update: Partial<WorkflowTaskDefinition>) => void; onToggleStart: () => void
  confirmDelete: boolean; onRequestDelete: () => void; onCancelDelete: () => void; onDelete: () => void
}) {
  const { t } = useI18n()
  return <div className="property-form"><span>{t('taskSettings')}</span><h2>{task.title}</h2><small className="internal-id">{task.id}</small><label>{t('title')}<input value={task.title} onChange={event => onChange({ title: event.target.value })} /></label><label>{t('description')}<textarea rows={3} value={task.description} onChange={event => onChange({ description: event.target.value })} /></label><label>{t('acceptanceCriteria')}<textarea rows={3} value={task.acceptance_criteria} onChange={event => onChange({ acceptance_criteria: event.target.value })} /></label><div className="property-row"><label>{t('executor')}<select value={task.executor} onChange={event => onChange({ executor: event.target.value as WorkflowTaskDefinition['executor'] })}><option value="agent">{t('agent')}</option><option value="human">{t('human')}</option><option value="either">{t('either')}</option></select></label><label>{t('executionPolicy')}<select value={task.execution} onChange={event => onChange({ execution: event.target.value as WorkflowTaskDefinition['execution'] })}><option value="required">{t('requiredTask')}</option><option value="optional">{t('optionalTask')}</option></select></label></div><label>{t('reviewPolicy')}<select value={task.review_policy} onChange={event => onChange({ review_policy: event.target.value as WorkflowTaskDefinition['review_policy'] })}><option value="none">{t('reviewNone')}</option><option value="executor_decides">{t('reviewExecutorDecides')}</option><option value="required">{t('reviewRequiredPolicy')}</option></select></label><label>{t('role')}<CommaValuesInput values={task.allowed_roles} onChange={allowed_roles => onChange({ allowed_roles })} /></label><label>{t('tags')}<CommaValuesInput values={task.default_tags} onChange={default_tags => onChange({ default_tags })} /></label><ArtifactDefinitionsEditor artifacts={task.artifacts} onChange={artifacts => onChange({ artifacts })} /><label className="start-toggle"><input type="checkbox" checked={isStart} onChange={onToggleStart} /><span>{t('startNode')}</span></label><div className="property-danger">{confirmDelete ? <div className="inline-confirm"><p>{t('deleteTaskBody')}</p><button onClick={onCancelDelete}>{t('cancel')}</button><button className="danger-button" onClick={onDelete}>{t('deleteTask')}</button></div> : <button className="quiet-button danger-button" onClick={onRequestDelete}><Trash2 size={14} />{t('deleteTask')}</button>}</div></div>
}

function ArtifactDefinitionsEditor({ artifacts, onChange }: { artifacts: WorkflowTaskDefinition['artifacts']; onChange: (artifacts: WorkflowTaskDefinition['artifacts']) => void }) {
  const { t } = useI18n()
  return <section className="artifact-definitions"><div><strong>{t('expectedArtifacts')}</strong><button type="button" className="quiet-button" onClick={() => onChange([...artifacts, { name: '', description: '' }])}><Plus size={14} />{t('addArtifact')}</button></div><p>{t('expectedArtifactsBody')}</p>{artifacts.map((artifact, index) => <div className="artifact-definition" key={index}><label>{t('artifactName')}<input value={artifact.name} onChange={event => onChange(artifacts.map((item, itemIndex) => itemIndex === index ? { ...item, name: event.target.value } : item))} placeholder="commit" /></label><label>{t('artifactDescription')}<textarea rows={3} value={artifact.description} onChange={event => onChange(artifacts.map((item, itemIndex) => itemIndex === index ? { ...item, description: event.target.value } : item))} /></label><button type="button" className="quiet-button danger-button" onClick={() => onChange(artifacts.filter((_, itemIndex) => itemIndex !== index))}><Trash2 size={14} />{t('removeArtifact')}</button></div>)}</section>
}

function CommaValuesInput({ values, onChange }: { values: string[]; onChange: (values: string[]) => void }) {
  const [text, setText] = useState(values.join(', '))
  useEffect(() => { setText(values.join(', ')) }, [values])
  return <input value={text} onChange={event => setText(event.target.value)} onBlur={() => onChange(text.split(',').map(value => value.trim()).filter(Boolean))} />
}

function validationMessage(error: string) {
  const messages = {
    name: 'validationWorkflowName', tasks: 'validationWorkflowTasks', start: 'validationWorkflowStart', titles: 'validationTaskTitles',
    'execution-limit': 'validationExecutionLimit', 'duplicate-relation': 'validationDuplicateRelation', artifacts: 'validationArtifacts', 'duplicate-artifacts': 'validationDuplicateArtifacts',
  } as const
  return messages[error as keyof typeof messages] ?? 'validationWorkflowGraph'
}
