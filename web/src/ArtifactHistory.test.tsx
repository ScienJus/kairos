// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ArtifactHistory } from './ArtifactHistory'
import { I18nProvider } from './i18n'
import { api } from './api'
import type { Artifact, Task } from './types'

const identity = { id: 'reviewer', kind: 'human' as const, role: '' }
const timestamp = '2026-10-12T08:00:00Z'
function task(id: string, position: number): Task {
  return {
    id, position, work_item_id: 'work', workflow_task_id: 'edit', workflow_activation_id: `activation-${id}`,
    title: 'Edit report', status: 'completed', active_claim_id: null, parent_task_id: null,
    retry_of_task_id: null, retry_context: '', retry_instructions: '', decomposed_at: null,
    description: '', acceptance_criteria: '', executor: 'agent', skipped_by: null, skip_reason: '',
    allowed_roles: [], tags: [], reviews: [], failures: [], transition_decisions: [],
    submissions: [{ id: `submission-${id}`, task_id: id, claim_id: `claim-${id}`, result: `Result ${id}`, submitted_at: timestamp }],
    created_at: timestamp, updated_at: timestamp, completed_at: timestamp, execution: 'required', review_policy: 'none', version: 1,
  }
}
function artifact(source: Task, submissionID = source.submissions[0].id): Artifact {
  return { id: `artifact-${submissionID}`, task_id: source.id, work_item_id: 'work', claim_id: `claim-${source.id}`,
    submission_id: submissionID, name: 'report', uri: `https://example.test/${submissionID}`, created_at: timestamp }
}
beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks() })

it('distinguishes same-name loop artifacts by source and submission, collapsing older history', async () => {
  const first = task('first', 0)
  const second = task('second', 1)
  second.submissions.push({ id: 'revised', task_id: second.id, claim_id: 'new-claim', result: 'Revised report', submitted_at: timestamp })
  second.reviews = [{ id: 'review', task_id: second.id, submission_id: second.submissions[0].id, status: 'rejected',
    requested_by: 'agent', requested_at: timestamp, decided_by: 'reviewer', decided_at: timestamp, feedback: 'Include missing evidence.' }]
  render(<I18nProvider><ArtifactHistory tasks={[first, second]} artifacts={[artifact(first), artifact(second), artifact(second, 'revised')]} identity={identity} /></I18nProvider>)
  const history = screen.getByRole('region', { name: 'Artifact history' })
  const previous = within(history).getByText('Execution 1').closest('.artifact-execution') as HTMLElement
  const current = within(history).getByText('Execution 2').closest('.artifact-execution') as HTMLElement
  expect(previous).not.toHaveAttribute('open')
  expect(within(history).getAllByText('Edit report')).toHaveLength(1)
  expect(history.querySelectorAll('.artifact-node-history')).toHaveLength(1)
  await userEvent.setup().click(within(previous).getByText('Execution 1'))
  expect(previous).toHaveAttribute('open')
  expect(within(previous).getByRole('link', { name: 'Open source task' })).toHaveAttribute('href', '/work-items/work/tasks/first')
  expect(current).toHaveAttribute('open')
  const rejected = within(current).getByText('Rejected').closest('details')!
  expect(rejected).not.toHaveAttribute('open')
  await userEvent.setup().click(within(rejected).getByText('Submission 1'))
  expect(rejected).toHaveAttribute('open')
  expect(within(rejected).getByText('Include missing evidence.')).toBeInTheDocument()
  expect(within(rejected).getByRole('link')).toHaveAttribute('href', 'https://example.test/submission-second')
  expect(history.querySelectorAll('time')).toHaveLength(3)
  expect(within(history).getAllByText('report')).toHaveLength(3)
})

it('shows pending review beside its managed artifact and reports download errors', async () => {
  const source = task('pending', 0)
  source.reviews = [{ id: 'review', task_id: source.id, submission_id: source.submissions[0].id, status: 'pending',
    requested_by: 'agent', requested_at: timestamp, decided_by: null, decided_at: null, feedback: '' }]
  const value = { ...artifact(source), uri: 'kairos://blobs/pending' }
  vi.spyOn(api, 'downloadArtifact').mockRejectedValue(new Error('Download unavailable'))
  render(<I18nProvider><ArtifactHistory tasks={[source]} artifacts={[value]} identity={identity} taskSpecific /></I18nProvider>)
  expect(screen.getByRole('region', { name: 'Task artifacts' })).toBeInTheDocument()
  expect(screen.getByText('Review')).toBeInTheDocument()
  expect(screen.getByText('Requested by agent')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: 'Download' }))
  expect(await screen.findByText('Download unavailable')).toBeInTheDocument()
})

it('keeps equally titled Workflow nodes separate and counts artifact-free attempts', () => {
  const first = task('first', 0)
  const failed = { ...task('failed', 1), status: 'failed' as const, submissions: [], completed_at: null, failures: [{ id: 'failure', task_id: 'failed', claim_id: 'failed-claim', action: 'await_human' as const, reason: 'Needs help', retry_prompt: '', failed_at: timestamp }] }
  const third = task('third', 2)
  const otherNode = { ...task('other', 3), workflow_task_id: 'other-node' }
  render(<I18nProvider><ArtifactHistory tasks={[third, otherNode, first, failed]} artifacts={[artifact(first), artifact(third), artifact(otherNode)]} identity={identity} /></I18nProvider>)
  const history = screen.getByRole('region', { name: 'Artifact history' })
  expect(history.querySelectorAll('.artifact-node-history')).toHaveLength(2)
  expect(within(history).getAllByText('Edit report')).toHaveLength(2)
  expect(screen.getByText('Execution 3')).toBeInTheDocument()
  expect(screen.queryByText('Execution 2')).not.toBeInTheDocument()
  expect(screen.getAllByText('Execution 1')).toHaveLength(2)
})

it('keeps equally titled Blackboard tasks independent', () => {
  const first = { ...task('first', 0), workflow_task_id: null, workflow_activation_id: null, execution: null, review_policy: null }
  const second = { ...task('second', 1), workflow_task_id: null, workflow_activation_id: null, execution: null, review_policy: null }
  render(<I18nProvider><ArtifactHistory tasks={[first, second]} artifacts={[artifact(first), artifact(second)]} identity={identity} /></I18nProvider>)
  const history = screen.getByRole('region', { name: 'Artifact history' })
  expect(history.querySelectorAll('.artifact-node-history')).toHaveLength(2)
  expect(within(history).getAllByText('Edit report')).toHaveLength(2)
  expect(history.querySelectorAll('.artifact-execution')).toHaveLength(0)
  expect(within(history).getAllByRole('link', { name: 'Open source task' }).map(link => link.getAttribute('href'))).toEqual(['/work-items/work/tasks/first', '/work-items/work/tasks/second'])
})
