import { Paperclip } from 'lucide-react'
import { useI18n } from './i18n'
import { ArtifactLinks } from './TaskArtifacts'
import { Status } from './ui'
import type { Artifact, Identity, Task } from './types'

export function ArtifactHistory({ artifacts, tasks, identity, taskSpecific = false }: {
  artifacts: Artifact[]; tasks: Task[]; identity: Identity; taskSpecific?: boolean
}) {
  const { t, formatTime } = useI18n()
  const orderedTasks = [...tasks].sort((a, b) => a.position - b.position)
  // Workflow groups use the stable Definition node ID. Blackboard Tasks remain
  // independent, even when titles match. Include artifact-free instances when
  // numbering executions so failed or pending attempts do not renumber history.
  const nodes = new Map<string, Task[]>()
  for (const task of orderedTasks) {
    const key = task.workflow_task_id === null ? `task:${task.id}` : `node:${task.workflow_task_id}`
    const executions = nodes.get(key) ?? []
    executions.push(task)
    nodes.set(key, executions)
  }
  const bySubmission = new Map<string, Artifact[]>()
  for (const artifact of artifacts) {
    if (artifact.submission_id !== null) {
      const values = bySubmission.get(artifact.submission_id) ?? []
      values.push(artifact)
      bySubmission.set(artifact.submission_id, values)
    }
  }
  const hasArtifacts = (task: Task) => task.submissions.some(submission => bySubmission.has(submission.id))
  const sourceLink = (task: Task) => <a href={`/work-items/${encodeURIComponent(task.work_item_id)}/tasks/${encodeURIComponent(task.id)}`}>{t('artifactSource')}</a>
  const submissions = (task: Task) => [...task.submissions].reverse().map((submission, reverseIndex) => {
    const values = bySubmission.get(submission.id)
    if (!values) return null
    const review = task.reviews.find(item => item.submission_id === submission.id)
    const status = review ? t(review.status === 'approved' ? 'statusApproved' : review.status === 'rejected' ? 'statusRejected' : 'statusReview') : t('artifactNoReview')
    return <details className="artifact-submission" key={submission.id} open={reverseIndex === 0}>
      <summary>
        <strong>{t('artifactSubmission', { number: task.submissions.length - reverseIndex })}</strong>
        <span>{status}</span><time dateTime={submission.submitted_at}>{formatTime(submission.submitted_at)}</time>
      </summary>
      <div className="artifact-submission-body">
        <p>{submission.result}</p>
        {review && <p className="artifact-review-feedback">{review.feedback || t('requestedBy', { actor: review.requested_by })}</p>}
        <ArtifactLinks artifacts={values} identity={identity} />
      </div>
    </details>
  })
  const label = t(taskSpecific ? 'taskArtifacts' : 'workArtifacts')
  return <section className="task-artifacts artifact-history" aria-label={label}>
    {taskSpecific && <div className="task-artifacts-heading"><Paperclip size={15} /><span>{label}</span></div>}
    {[...nodes].filter(([, executions]) => executions.some(hasArtifacts)).map(([key, executions]) => (
      <div className="artifact-node-history" key={key}>
        {!taskSpecific && <div className="artifact-source-heading">
          <strong>{executions[0].title}</strong>
          {executions[0].workflow_task_id === null && sourceLink(executions[0])}
        </div>}
        {[...executions].reverse().map((task, reverseIndex) => {
          if (!hasArtifacts(task)) return null
          if (taskSpecific || task.workflow_task_id === null) return <div className="artifact-task-history" key={task.id}>{submissions(task)}</div>
          return <details className="artifact-execution" key={task.id} open={reverseIndex === 0}>
            <summary><strong>{t('artifactExecution', { number: executions.length - reverseIndex })}</strong><Status value={task.status} /></summary>
            <div className="artifact-execution-body">
              <div className="artifact-source-heading">{sourceLink(task)}</div>
              {submissions(task)}
            </div>
          </details>
        })}
      </div>
    ))}
  </section>
}
