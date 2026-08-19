import { useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Bot, UserRound, X, XCircle } from 'lucide-react'
import { APIError } from './api'
import { api } from './api'
import { useI18n } from './i18n'
import { useWorkItemData, useWorkflowDefinitionData } from './pageData'
import type { HomeView, RouteState } from './route'
import { TaskDetail, BlackboardCompletionActions, CreateTask, EmptyBlackboardActions } from './TaskDetail'
import { TaskMap } from './TaskMap'
import type { Identity } from './types'
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
  const workflowBinding = context.data?.WorkItem.Definition.Mode === 'workflow' ? context.data.WorkItem.Definition : null
  const workflowDefinition = useWorkflowDefinitionData(identity, workflowBinding?.ID ?? null, workflowBinding?.Version ?? null)
  const selectedTask = context.data?.Tasks.find(task => task.ID === selectedTaskID) ?? null
  const selectedTaskClaim = selectedTask ? context.data?.ActiveClaims.find(claim => claim.TaskID === selectedTask.ID) ?? null : null
  const selectedTaskExecutionClaim = selectedTask?.Submissions.at(-1)?.ClaimID ? context.data?.Claims.find(claim => claim.ID === selectedTask.Submissions.at(-1)?.ClaimID) ?? null : null
  const pendingReviews = context.data?.Tasks.flatMap(task => (task.Reviews ?? []).filter(review => review.Status === 'pending')) ?? []
  const queryClient = useQueryClient()
  const accept = useMutation({ mutationFn: () => api.acceptBlackboardCompletion(identity, workItemID), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['work-item', identity, workItemID] }); queryClient.invalidateQueries({ queryKey: ['human-attention', identity] }) } })
  const blackboardConverged = context.data?.WorkItem.Definition.Mode === 'blackboard' && context.data.WorkItem.Status === 'open' && context.data.Tasks.length > 0 && context.data.Tasks.every(task => task.Status === 'completed' || task.Status === 'skipped')

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
        <div className="work-hero"><button className="back-button" onClick={() => navigate({ workItemID: null, taskID: null, homeView })}><ArrowLeft size={17} />{t('putDown')}</button><div className="hero-title"><div><Status value={context.data.WorkItem.Status} /><h1>{context.data.WorkItem.Title}</h1></div></div>
          <p className="goal">{context.data.WorkItem.Goal}</p>{pendingReviews.length > 0 && <button className="review-summary" onClick={() => { const task = context.data.Tasks.find(item => (item.Reviews ?? []).some(review => review.Status === 'pending')); if (task) navigate({ workItemID, taskID: task.ID, homeView }) }}>{t('waitingReviews', { count: pendingReviews.length })}</button>}</div>
        {(context.data.WorkItem.AcceptanceCriteria || context.data.WorkItem.Context || context.data.WorkItem.Constraints) && <details className="work-brief"><summary>{t('readBrief')}</summary><div className="brief-grid">{context.data.WorkItem.Context && <Brief label={t('context')} value={context.data.WorkItem.Context} />}{context.data.WorkItem.AcceptanceCriteria && <Brief label={t('doneWhen')} value={context.data.WorkItem.AcceptanceCriteria} />}{context.data.WorkItem.Constraints && <Brief label={t('keepInMind')} value={context.data.WorkItem.Constraints} />}</div></details>}
        {context.data.WorkItem.Status === 'awaiting_human_acceptance' && <HumanAcceptance result={context.data.WorkItem.Result} error={accept.error} onAccept={() => accept.mutate()} pending={accept.isPending} />}
        <div className="task-section"><div className="section-heading"><div><h2>{t(context.data.WorkItem.Definition.Mode === 'workflow' ? 'workflowTitle' : 'blackboardTitle')}</h2><p>{t(context.data.WorkItem.Definition.Mode === 'workflow' ? 'workflowBody' : 'blackboardBody')}</p></div>{context.data.WorkItem.Definition.Mode === 'blackboard' && context.data.WorkItem.Status === 'open' && context.data.Tasks.length > 0 && <CreateTask identity={identity} workItemID={workItemID} />}</div>
          {context.data.WorkItem.Definition.Mode === 'blackboard' && context.data.WorkItem.Status === 'open' && context.data.Tasks.length === 0 && <EmptyBlackboardActions identity={identity} workItemID={workItemID} />}
          {blackboardConverged && <BlackboardCompletionActions identity={identity} workItemID={workItemID} />}
          <TaskMap mode={context.data.WorkItem.Definition.Mode} tasks={context.data.Tasks} relations={context.data.Relations} workflowDefinition={workflowDefinition.data} selectedTaskID={selectedTaskID} onSelect={taskID => navigate({ workItemID, taskID, homeView })} />
        </div>
      </>}
    </section>

    {selectedTask && <><button className="inspector-backdrop" aria-label={t('closeTask')} onClick={() => navigate({ workItemID, taskID: null, homeView })} /><aside className="task-panel mobile-task">
      <div className="panel-header"><div><span className="eyebrow">{t('selectedTask')}</span><h2>{t('taskDetails')}</h2></div><div className="panel-actions">{selectedTask.Executor === 'agent' ? <Bot size={20} /> : <UserRound size={20} />}<button className="icon-button" onClick={() => navigate({ workItemID, taskID: null, homeView })} aria-label={t('closeTask')}><X size={17} /></button></div></div>
      <TaskDetail key={selectedTask.ID} task={selectedTask} activeClaim={selectedTaskClaim} executionClaim={selectedTaskExecutionClaim} identity={identity} mode={context.data!.WorkItem.Definition.Mode} />
    </aside></>}
  </>
}

function Brief({ label, value }: { label: string; value: string }) { return <div className="brief"><span>{label}</span><p>{value || '—'}</p></div> }
function HumanAcceptance({ result, error, onAccept, pending }: { result: string; error: Error | null; onAccept: () => void; pending: boolean }) {
  const { t } = useI18n()
  return <div className="empty-planning"><h3>{t('humanAcceptance')}</h3><p>{t('humanAcceptanceBody')}</p><div className="brief"><span>{t('completionResult')}</span><p>{result}</p></div>{error && <FormError error={error} />}<button className="quiet-button" disabled={pending} onClick={onAccept}>{t('approveAcceptance')}</button></div>
}
function PanelPlaceholder({ loading }: { loading: boolean }) { const { t } = useI18n(); return <div className="panel-placeholder"><div className="target-reticle"><i /><span /></div><strong>{t(loading ? 'acquiring' : 'noWork')}</strong></div> }
