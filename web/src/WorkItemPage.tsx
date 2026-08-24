import { useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Bot, ChevronLeft, ChevronRight, UserRound, X, XCircle } from 'lucide-react'
import { APIError } from './api'
import { api } from './api'
import { useI18n } from './i18n'
import { useWorkItemData, useWorkflowDefinitionData } from './pageData'
import type { HomeView, RouteState } from './route'
import { TaskDetail, BlackboardCompletionActions, CreateTask, EmptyBlackboardActions } from './TaskDetail'
import { TaskMap } from './TaskMap'
import type { Identity, Task } from './types'
import { FormError, Status } from './ui'

export function WorkItemPage({ identity, workItemID, selectedTaskID, homeView, navigate }: {
  identity: Identity
  workItemID: string
  selectedTaskID: string | null
  homeView: HomeView
  navigate: (route: RouteState, replace?: boolean) => void
}) {
  const { t } = useI18n()
  const context = useWorkItemData(identity, workItemID)
  const workflowBinding = context.data?.work_item.definition.mode === 'workflow' ? context.data.work_item.definition : null
  const workflowDefinition = useWorkflowDefinitionData(identity, workflowBinding?.id ?? null, workflowBinding?.version ?? null)
  const selectedTask = context.data?.tasks.find(task => task.id === selectedTaskID) ?? null
  const selectedWorkflowNodeTasks = selectedTask?.workflow_task_id
    ? context.data?.tasks.filter(task => task.workflow_task_id === selectedTask.workflow_task_id).sort((left, right) => left.position - right.position) ?? []
    : []
  const selectedTaskClaim = selectedTask ? context.data?.active_claims.find(claim => claim.task_id === selectedTask.id) ?? null : null
  const selectedTaskExecutionClaim = selectedTask?.submissions.at(-1)?.claim_id ? context.data?.claims.find(claim => claim.id === selectedTask.submissions.at(-1)?.claim_id) ?? null : null
  const pendingReviews = context.data?.tasks.flatMap(task => (task.reviews ?? []).filter(review => review.status === 'pending')) ?? []
  const queryClient = useQueryClient()
  const accept = useMutation({ mutationFn: () => api.acceptBlackboardCompletion(identity, workItemID), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['work-item', identity, workItemID] }); queryClient.invalidateQueries({ queryKey: ['human-attention', identity] }) } })
  const blackboardConverged = context.data?.work_item.definition.mode === 'blackboard' && context.data.work_item.status === 'open' && context.data.tasks.length > 0 && context.data.tasks.every(task => task.status === 'completed' || task.status === 'skipped')

  useEffect(() => {
    if (context.data && selectedTaskID && !selectedTask) navigate({ workItemID, taskID: null, homeView }, true)
  }, [context.data, selectedTask, selectedTaskID, workItemID, homeView])

  useEffect(() => {
    if (!selectedTaskID) return
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') navigate({ workItemID, taskID: null, homeView })
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [selectedTaskID, workItemID, homeView])

  if (context.error) return <div className="error-banner"><XCircle size={16} /><span>{context.error instanceof APIError ? context.error.message : t('unreachable')}</span></div>

  return <>
    <section className="work-panel mobile-work">
      {!context.data && <PanelPlaceholder loading={context.isLoading} />}
      {context.data && <>
        <div className="work-hero"><button className="back-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView })}><ArrowLeft size={17} />{t('putDown')}</button><div className="hero-title"><div><Status value={context.data.work_item.status} /><h1>{context.data.work_item.title}</h1></div></div>
          <p className="goal">{context.data.work_item.goal}</p>{pendingReviews.length > 0 && <button className="review-summary" onClick={() => { const task = context.data.tasks.find(item => (item.reviews ?? []).some(review => review.status === 'pending')); if (task) navigate({ workItemID, taskID: task.id, homeView }) }}>{t('waitingReviews', { count: pendingReviews.length })}</button>}</div>
        {(context.data.work_item.acceptance_criteria || context.data.work_item.context || context.data.work_item.constraints) && <details className="work-brief"><summary>{t('readBrief')}</summary><div className="brief-grid">{context.data.work_item.context && <Brief label={t('context')} value={context.data.work_item.context} />}{context.data.work_item.acceptance_criteria && <Brief label={t('doneWhen')} value={context.data.work_item.acceptance_criteria} />}{context.data.work_item.constraints && <Brief label={t('keepInMind')} value={context.data.work_item.constraints} />}</div></details>}
        {context.data.artifacts.length > 0 && <details className="work-brief"><summary>{t('workArtifacts')}</summary><div className="artifact-list">{context.data.artifacts.map(artifact => <div key={artifact.id}><strong>{artifact.name}</strong>{artifact.uri.startsWith('kairos://') ? <button className="quiet-button" onClick={async () => { const blob = await api.downloadArtifact(identity, artifact.id); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = artifact.name; link.click(); URL.revokeObjectURL(url) }}>{t('downloadArtifact')}</button> : /^https?:\/\//.test(artifact.uri) ? <a href={artifact.uri} target="_blank" rel="noreferrer">{artifact.uri}</a> : <span>{artifact.uri}</span>}</div>)}</div></details>}
        {context.data.work_item.status === 'awaiting_human_acceptance' && <HumanAcceptance result={context.data.work_item.result} error={accept.error} onAccept={() => accept.mutate()} pending={accept.isPending} />}
        <div className="task-section"><div className="section-heading"><div><h2>{t(context.data.work_item.definition.mode === 'workflow' ? 'workflowTitle' : 'blackboardTitle')}</h2><p>{t(context.data.work_item.definition.mode === 'workflow' ? 'workflowBody' : 'blackboardBody')}</p></div>{context.data.work_item.definition.mode === 'blackboard' && context.data.work_item.status === 'open' && context.data.tasks.length > 0 && <CreateTask identity={identity} workItemID={workItemID} />}</div>
          {context.data.work_item.definition.mode === 'blackboard' && context.data.work_item.status === 'open' && context.data.tasks.length === 0 && <EmptyBlackboardActions identity={identity} workItemID={workItemID} />}
          {blackboardConverged && <BlackboardCompletionActions identity={identity} workItemID={workItemID} />}
          <TaskMap mode={context.data.work_item.definition.mode} tasks={context.data.tasks} relations={context.data.relations} workflowDefinition={workflowDefinition.data} selectedTaskID={selectedTaskID} onSelect={taskID => navigate({ workItemID, taskID, homeView })} />
        </div>
      </>}
    </section>

    {selectedTask && <><button className="inspector-backdrop" aria-label={t('closeTask')} onClick={() => navigate({ workItemID, taskID: null, homeView })} /><aside className="task-panel mobile-task">
      <div className="panel-header"><div><span className="eyebrow">{t('selectedTask')}</span><h2>{t(selectedTask.workflow_task_id ? 'workflowNodeDetails' : 'taskDetails')}</h2></div><div className="panel-actions">{selectedTask.executor === 'agent' ? <Bot size={20} /> : <UserRound size={20} />}<button className="icon-button" onClick={() => navigate({ workItemID, taskID: null, homeView })} aria-label={t('closeTask')}><X size={17} /></button></div></div>
      <WorkflowInstancePager tasks={selectedWorkflowNodeTasks} selectedTaskID={selectedTask.id} onSelect={taskID => navigate({ workItemID, taskID, homeView })} />
      <TaskDetail key={selectedTask.id} task={selectedTask} activeClaim={selectedTaskClaim} executionClaim={selectedTaskExecutionClaim} identity={identity} mode={context.data!.work_item.definition.mode} />
    </aside></>}
  </>
}

export function WorkflowInstancePager({ tasks, selectedTaskID, onSelect }: { tasks: Task[]; selectedTaskID: string; onSelect: (taskID: string) => void }) {
  const { t } = useI18n()
  if (tasks.length <= 1) return null
  const current = tasks.findIndex(task => task.id === selectedTaskID)
  if (current < 0) return null
  const previous = tasks[current - 1]
  const next = tasks[current + 1]
  return <nav className="workflow-instance-pager" aria-label={t('workflowInstanceHistory')}>
    <span>{t('workflowInstancePosition', { current: current + 1, total: tasks.length })}</span>
    <div>
      <button type="button" disabled={!previous} title={t('previousWorkflowInstance')} aria-label={t('previousWorkflowInstance')} onClick={() => previous && onSelect(previous.id)}><ChevronLeft size={16} /></button>
      <button type="button" disabled={!next} title={t('nextWorkflowInstance')} aria-label={t('nextWorkflowInstance')} onClick={() => next && onSelect(next.id)}><ChevronRight size={16} /></button>
    </div>
  </nav>
}

function Brief({ label, value }: { label: string; value: string }) { return <div className="brief"><span>{label}</span><p>{value || '—'}</p></div> }
function HumanAcceptance({ result, error, onAccept, pending }: { result: string; error: Error | null; onAccept: () => void; pending: boolean }) {
  const { t } = useI18n()
  return <div className="empty-planning"><h3>{t('humanAcceptance')}</h3><p>{t('humanAcceptanceBody')}</p><div className="brief"><span>{t('completionResult')}</span><p>{result}</p></div>{error && <FormError error={error} />}<button className="quiet-button" disabled={pending} onClick={onAccept}>{t('approveAcceptance')}</button></div>
}
function PanelPlaceholder({ loading }: { loading: boolean }) { const { t } = useI18n(); return <div className="panel-placeholder"><div className="target-reticle"><i /><span /></div><strong>{t(loading ? 'acquiring' : 'noWork')}</strong></div> }
