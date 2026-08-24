// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { I18nProvider } from './i18n'
import { TaskDetail } from './TaskDetail'
import type { Claim, Identity, Task, TaskDetailView, TaskExecutionContext, WorkItem } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }
const claim: Claim = {
  id: 'claim-1', task_id: 'task-1', executor: { kind: 'human', id: identity.id },
  claimed_at: '2026-08-17T08:00:00Z', last_heartbeat_at: '0001-01-01T00:00:00Z',
  lease_until: '0001-01-01T00:00:00Z', lease_seconds: 0, ended_at: null, end_reason: '',
}

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1', work_item_id: 'work-1', status: 'working', active_claim_id: claim.id, parent_task_id: null,
    workflow_task_id: null, workflow_activation_id: null, decomposed_at: null,
    title: 'Prepare release', description: 'Prepare the release notes.', acceptance_criteria: 'Notes are ready.',
    executor: 'human', allowed_roles: [], tags: [], reviews: [], submissions: [], failures: [],
    transition_decisions: [], position: 0, created_at: '2026-08-17T08:00:00Z',
    updated_at: '2026-08-17T08:00:00Z', completed_at: null, skipped_by: null, skip_reason: '',
    execution: null, review_policy: null, version: 1,
    ...overrides,
  }
}

function makeWorkItem(): WorkItem {
  return {
    id: 'work-1', definition: { id: 'definition-1', version: 1, mode: 'blackboard' }, status: 'open', acceptance_mode: 'none',
    title: 'Release', goal: 'Ship safely', context: '', constraints: '', acceptance_criteria: '', tags: [], result: '',
    version: 1, created_at: '2026-08-17T08:00:00Z', updated_at: '2026-08-17T08:00:00Z', completed_at: null,
    cancelled_at: null, cancelled_by: null, cancellation_reason: '',
  }
}

function execution(task: Task, claims: Claim[] = []): TaskExecutionContext {
  const actor = task.skipped_by ?? claims[0]?.executor ?? null
  const kind = task.status === 'skipped' ? 'skipped_by' : task.status === 'completed' ? 'executed_by' : task.status === 'working' ? 'claimed_by' : 'unclaimed'
  return { work_item: makeWorkItem(), task: task, claims: claims, artifacts: [], expected_artifacts: [], responsibility: { kind: kind, actor: actor }, outcome: { kind: task.status, actor: actor, reason: task.skip_reason, occurred_at: task.completed_at }, workflow: null, blackboard: { tasks: [task], relations: [], can_decompose: false } }
}

function detail(task: Task, claims: Claim[] = []): TaskDetailView {
  const actor = task.skipped_by ?? claims[0]?.executor ?? null
  const kind = task.status === 'skipped' ? 'skipped_by' : task.status === 'completed' ? 'executed_by' : task.status === 'working' ? 'claimed_by' : 'unclaimed'
  const ownsClaim = claims.some(item => !item.ended_at && item.executor.kind === identity.kind && item.executor.id === identity.id)
  const canExecute = task.executor === 'human' || task.executor === 'either'
  return {
    task: task, responsibility: { kind: kind, actor: actor }, outcome: { kind: task.status, actor: actor, reason: task.skip_reason, occurred_at: task.completed_at }, current_review: task.reviews.at(-1) ?? null,
    history: { claims: claims, submissions: task.submissions, reviews: task.reviews, failures: task.failures, transition_decisions: task.transition_decisions }, artifacts: [],
    capabilities: { can_claim: task.status === 'pending' && canExecute, can_submit: task.status === 'working' && ownsClaim, can_release: task.status === 'working' && ownsClaim, can_fail: task.status === 'working' && ownsClaim, can_review: task.status === 'in_review', can_skip: task.status === 'pending', can_decompose: false, can_add_child: task.status === 'waiting_children' },
  }
}

function renderTask(task: Task, activeClaim: Claim | null = null, mode = 'blackboard', executionClaim: Claim | null = null) {
  vi.spyOn(api, 'getTaskDetail').mockResolvedValue(detail(task, activeClaim ? [activeClaim] : executionClaim ? [executionClaim] : []))
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return Object.assign(
    render(<QueryClientProvider client={queryClient}><I18nProvider><TaskDetail task={task} activeClaim={activeClaim} executionClaim={executionClaim} identity={identity} mode={mode} /></I18nProvider></QueryClientProvider>),
    { queryClient },
  )
}

beforeEach(() => {
  localStorage.setItem('kairos-console-locale', 'en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('Task detail operations', () => {
  it('shows artifacts delivered by the selected task', async () => {
    const task = makeTask({ status: 'completed', active_claim_id: null })
    const view = detail(task)
    view.artifacts = [
      { id: 'artifact-1', work_item_id: task.work_item_id, task_id: task.id, claim_id: 'claim-1', submission_id: 'submission-1', name: 'release-notes', uri: 'https://example.test/releases/notes', created_at: '2026-08-17T09:00:00Z' },
      { id: 'artifact-2', work_item_id: task.work_item_id, task_id: task.id, claim_id: 'claim-1', submission_id: 'submission-1', name: 'release-package', uri: 'kairos://blobs/sha256/abc', created_at: '2026-08-17T09:00:00Z' },
    ]
    vi.spyOn(api, 'getTaskDetail').mockResolvedValue(view)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(<QueryClientProvider client={queryClient}><I18nProvider><TaskDetail task={task} activeClaim={null} identity={identity} mode="workflow" /></I18nProvider></QueryClientProvider>)

    expect(await screen.findByRole('region', { name: 'Task artifacts' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /https:\/\/example\.test\/releases\/notes/ })).toHaveAttribute('href', 'https://example.test/releases/notes')
    expect(screen.getByRole('button', { name: 'Download' })).toBeInTheDocument()
    expect(screen.getByText('release-notes')).toBeInTheDocument()
    expect(screen.getByText('release-package')).toBeInTheDocument()
  })

  it('shows the executor from the completed task submission claim', async () => {
    const endedClaim = { ...claim, executor: { kind: 'agent' as const, id: 'codex-backend' }, ended_at: '2026-08-17T09:00:00Z', end_reason: 'submitted' }
    const task = makeTask({
      status: 'completed', active_claim_id: null, completed_at: '2026-08-17T09:00:00Z',
      submissions: [{ id: 'submission-1', task_id: 'task-1', claim_id: endedClaim.id, result: 'Done', submitted_at: '2026-08-17T09:00:00Z' }],
    })
    const getExecutionContext = vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [endedClaim]))

    renderTask(task, null, 'blackboard', endedClaim)

    expect(await screen.findByText('Executed by')).toBeInTheDocument()
    expect(await screen.findByText('codex-backend')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
    expect(getExecutionContext).not.toHaveBeenCalled()
  })

  it('shows the skip decision instead of an unclaimed executor', async () => {
    const task = makeTask({ status: 'skipped', active_claim_id: null, completed_at: '2026-08-17T09:00:00Z', skipped_by: { kind: 'agent', id: 'planner-1' }, skip_reason: 'Covered by another task' })
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task))

    renderTask(task)

    expect(await screen.findByText('Skipped by')).toBeInTheDocument()
    expect(await screen.findByText('planner-1')).toBeInTheDocument()
    expect(await screen.findByText('Covered by another task')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
  })

  it('uses lifecycle-specific responsibility labels and does not call a missing skip actor unclaimed', async () => {
    const task = makeTask({ status: 'skipped', active_claim_id: null, skipped_by: null })
    renderTask(task)

    expect(await screen.findByText('Skipped by')).toBeInTheDocument()
    expect(screen.getByText('Not recorded')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
  })

  it('renders safely when optional task collections are empty', async () => {
    const task = makeTask({ status: 'pending', active_claim_id: null, reviews: [], tags: [], submissions: [], failures: [] })
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task))

    renderTask(task)

    const startButtons = await screen.findAllByRole('button', { name: 'Start task' })
    expect(startButtons.find(button => button.hasAttribute('aria-expanded'))).toHaveAttribute('aria-expanded', 'true')
    expect(screen.queryByText('Review channel')).not.toBeInTheDocument()
    expect(screen.queryByText('Failure history')).not.toBeInTheDocument()
  })

  it('keeps entered completion text when switching between operation panels', async () => {
    const task = makeTask()
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [claim]))
    const user = userEvent.setup()

    renderTask(task, claim)

    const result = await screen.findByRole('textbox', { name: 'What was accomplished?' })
    await user.type(result, 'Release notes drafted')
    await user.click(screen.getByRole('button', { name: 'Put down for now' }))
    expect(screen.queryByRole('textbox', { name: 'What was accomplished?' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Complete this task' }))
    expect(screen.getByRole('textbox', { name: 'What was accomplished?' })).toHaveValue('Release notes drafted')
  })

  it('uploads a managed file and submits its Artifact ID', async () => {
    const task = makeTask()
    const context = execution(task, [claim])
    context.work_item.definition.mode = 'workflow'
    context.expected_artifacts = [{ name: 'release-package', description: 'Upload the release archive.' }]
    context.workflow = { upstream_tasks: [], choice_groups: [] }
    context.blackboard = null
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(context)
    const artifact = {
      id: 'artifact-uploaded', work_item_id: task.work_item_id, task_id: task.id, claim_id: claim.id,
      submission_id: null, name: 'release-package', uri: 'kairos://blobs/sha256/abc', created_at: '2026-08-17T09:00:00Z',
    }
    const upload = vi.spyOn(api, 'uploadArtifact').mockResolvedValue(artifact)
    const submit = vi.spyOn(api, 'submitTask').mockResolvedValue({
      id: 'submission-1', task_id: task.id, claim_id: claim.id, result: 'Release ready', submitted_at: '2026-08-17T09:01:00Z',
    })
    const user = userEvent.setup()
    const file = new File(['release bytes'], 'release.zip', { type: 'application/zip' })

    renderTask(task, claim, 'workflow')

    await user.type(await screen.findByRole('textbox', { name: 'What was accomplished?' }), 'Release ready')
    await user.upload(screen.getByLabelText('release-package: Upload file'), file)
    await user.click(screen.getByRole('button', { name: 'Complete task' }))

    await waitFor(() => expect(upload).toHaveBeenCalledWith(identity, task.id, claim.id, 'release-package', file, expect.any(String)))
    expect(submit).toHaveBeenCalledWith(identity, task.id, expect.objectContaining({
      claim_id: claim.id, artifact_ids: [artifact.id], result: 'Release ready',
    }))
  })

  it('reuses the managed upload operation ID for an identical retry', async () => {
    const task = makeTask()
    const context = execution(task, [claim])
    context.work_item.definition.mode = 'workflow'
    context.expected_artifacts = [{ name: 'release-package', description: 'Upload the release archive.' }]
    context.workflow = { upstream_tasks: [], choice_groups: [] }
    context.blackboard = null
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(context)
    const artifact = {
      id: 'artifact-uploaded', work_item_id: task.work_item_id, task_id: task.id, claim_id: claim.id,
      submission_id: null, name: 'release-package', uri: 'kairos://blobs/uploads/retry', created_at: '2026-08-17T09:00:00Z',
    }
    const upload = vi.spyOn(api, 'uploadArtifact')
      .mockRejectedValueOnce(new Error('Upload interrupted'))
      .mockResolvedValueOnce(artifact)
    vi.spyOn(api, 'submitTask').mockResolvedValue({
      id: 'submission-1', task_id: task.id, claim_id: claim.id, result: 'Release ready', submitted_at: '2026-08-17T09:01:00Z',
    })
    const user = userEvent.setup()
    const file = new File(['release bytes'], 'release.zip', { type: 'application/zip' })

    renderTask(task, claim, 'workflow')

    await user.type(await screen.findByRole('textbox', { name: 'What was accomplished?' }), 'Release ready')
    await user.upload(screen.getByLabelText('release-package: Upload file'), file)
    await user.click(screen.getByRole('button', { name: 'Complete task' }))
    expect(await screen.findByText('Upload interrupted')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Complete task' }))

    await waitFor(() => expect(upload).toHaveBeenCalledTimes(2))
    expect(upload.mock.calls[1][5]).toBe(upload.mock.calls[0][5])
  })

  it('clears staged Artifact state when the active Claim changes', async () => {
    const task = makeTask()
    const firstContext = execution(task, [claim])
    firstContext.work_item.definition.mode = 'workflow'
    firstContext.expected_artifacts = [{ name: 'release-package', description: 'Upload the release archive.' }]
    firstContext.workflow = { upstream_tasks: [], choice_groups: [] }
    firstContext.blackboard = null
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(firstContext)
    const firstArtifact = {
      id: 'artifact-first-claim', work_item_id: task.work_item_id, task_id: task.id, claim_id: claim.id,
      submission_id: null, name: 'release-package', uri: 'kairos://blobs/uploads/first-claim', created_at: '2026-08-17T09:00:00Z',
    }
    const secondClaim = { ...claim, id: 'claim-2' }
    const secondArtifact = {
      ...firstArtifact, id: 'artifact-second-claim', claim_id: secondClaim.id, uri: 'kairos://blobs/uploads/second-claim',
    }
    const upload = vi.spyOn(api, 'uploadArtifact')
      .mockResolvedValueOnce(firstArtifact)
      .mockResolvedValueOnce(secondArtifact)
    const submit = vi.spyOn(api, 'submitTask')
      .mockRejectedValueOnce(new Error('Submission needs another attempt'))
      .mockResolvedValueOnce({
        id: 'submission-2', task_id: task.id, claim_id: secondClaim.id, result: 'Second result', submitted_at: '2026-08-17T10:01:00Z',
      })
    const user = userEvent.setup()
    const { queryClient } = renderTask(task, claim, 'workflow')

    await user.type(await screen.findByRole('textbox', { name: 'What was accomplished?' }), 'First result')
    await user.upload(screen.getByLabelText('release-package: Upload file'), new File(['first'], 'first.zip'))
    await user.click(screen.getByRole('button', { name: 'Complete task' }))
    expect(await screen.findByText('Submission needs another attempt')).toBeInTheDocument()

    const secondTask = { ...task, active_claim_id: secondClaim.id }
    const secondContext = execution(secondTask, [
      { ...claim, ended_at: '2026-08-17T09:30:00Z', end_reason: 'released' },
      secondClaim,
    ])
    secondContext.work_item.definition.mode = 'workflow'
    secondContext.expected_artifacts = firstContext.expected_artifacts
    secondContext.workflow = { upstream_tasks: [], choice_groups: [] }
    secondContext.blackboard = null
    act(() => queryClient.setQueryData(['task-context', identity, task.id], secondContext))

    const secondFileInput = await screen.findByLabelText('release-package: Upload file')
    await user.type(screen.getByRole('textbox', { name: 'What was accomplished?' }), 'Second result')
    await user.upload(secondFileInput, new File(['second'], 'second.zip'))
    await user.click(screen.getByRole('button', { name: 'Complete task' }))

    await waitFor(() => expect(upload).toHaveBeenCalledTimes(2))
    expect(upload.mock.calls[1][2]).toBe(secondClaim.id)
    expect(upload.mock.calls[1][5]).not.toBe(upload.mock.calls[0][5])
    expect(submit).toHaveBeenLastCalledWith(identity, task.id, expect.objectContaining({
      claim_id: secondClaim.id, artifact_ids: [secondArtifact.id], result: 'Second result',
    }))
  })

  it('submits only one staged Artifact for each name', async () => {
    const task = makeTask()
    const context = execution(task, [claim])
    context.work_item.definition.mode = 'workflow'
    context.expected_artifacts = [{ name: 'release-package', description: 'Upload the release archive.' }]
    context.artifacts = [
      { id: 'artifact-first', work_item_id: task.work_item_id, task_id: task.id, claim_id: claim.id, submission_id: null, name: 'release-package', uri: 'kairos://blobs/uploads/first', created_at: '2026-08-17T09:00:00Z' },
      { id: 'artifact-duplicate', work_item_id: task.work_item_id, task_id: task.id, claim_id: claim.id, submission_id: null, name: 'release-package', uri: 'kairos://blobs/uploads/duplicate', created_at: '2026-08-17T09:01:00Z' },
    ]
    context.workflow = { upstream_tasks: [], choice_groups: [] }
    context.blackboard = null
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(context)
    const upload = vi.spyOn(api, 'uploadArtifact')
    const submit = vi.spyOn(api, 'submitTask').mockResolvedValue({
      id: 'submission-1', task_id: task.id, claim_id: claim.id, result: 'Release ready', submitted_at: '2026-08-17T09:02:00Z',
    })
    const user = userEvent.setup()

    renderTask(task, claim, 'workflow')

    await user.type(await screen.findByRole('textbox', { name: 'What was accomplished?' }), 'Release ready')
    await user.click(screen.getByRole('button', { name: 'Complete task' }))

    await waitFor(() => expect(submit).toHaveBeenCalledWith(identity, task.id, expect.objectContaining({
      artifact_ids: ['artifact-first'],
    })))
    expect(upload).not.toHaveBeenCalled()
  })

  it('disables the completion action while its mutation is pending', async () => {
    const task = makeTask()
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [claim]))
    vi.spyOn(api, 'submitTask').mockImplementation(() => new Promise(() => undefined))
    const user = userEvent.setup()

    renderTask(task, claim)

    await user.type(await screen.findByRole('textbox', { name: 'What was accomplished?' }), 'Done')
    const submit = screen.getByRole('button', { name: 'Complete task' })
    await user.click(submit)

    await waitFor(() => expect(submit).toBeDisabled())
  })

  it('preserves the form and displays the API error when completion fails', async () => {
    const task = makeTask()
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [claim]))
    vi.spyOn(api, 'submitTask').mockRejectedValue(new Error('Result could not be submitted'))
    const user = userEvent.setup()

    renderTask(task, claim)

    const result = await screen.findByRole('textbox', { name: 'What was accomplished?' })
    await user.type(result, 'Keep this result')
    await user.click(screen.getByRole('button', { name: 'Complete task' }))

    expect(await screen.findByText('Result could not be submitted')).toBeInTheDocument()
    expect(result).toHaveValue('Keep this result')
  })

  it('can close the entire WorkItem from a claimed task failure', async () => {
    const task = makeTask()
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [claim]))
    const fail = vi.spyOn(api, 'failTask').mockResolvedValue(undefined)
    const user = userEvent.setup()

    renderTask(task, claim)

    await user.click(await screen.findByRole('button', { name: 'I could not complete this task' }))
    await user.type(screen.getByRole('textbox', { name: 'What prevented completion?' }), 'The requested outcome is impossible')
    await user.click(screen.getByRole('radio', { name: /Close the entire work as failed/ }))
    expect(screen.queryByRole('textbox', { name: 'What should the next attempt know?' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Close work as failed' }))

    await waitFor(() => expect(fail).toHaveBeenCalledWith(identity, task.id, {
      claim_id: claim.id, action: 'fail_work_item', reason: 'The requested outcome is impossible', retry_prompt: '',
    }))
  })

  it('opens review and Blackboard planning actions for their matching states', async () => {
    const reviewTask = makeTask({
      status: 'in_review', active_claim_id: null,
      reviews: [{ id: 'review-1', task_id: 'task-1', submission_id: null, status: 'pending', requested_by: 'agent-1', requested_at: '2026-08-17T08:00:00Z', decided_by: null, decided_at: null, feedback: '' }],
    })
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(reviewTask))
    const { unmount } = renderTask(reviewTask)
    expect(await screen.findByRole('button', { name: 'Review this result' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled()
    unmount()

    const agentTask = makeTask({ status: 'pending', active_claim_id: null, executor: 'agent' })
    renderTask(agentTask)
    expect(await screen.findByRole('button', { name: 'This is no longer needed' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('textbox', { name: 'Why is it no longer needed?' })).toBeInTheDocument()
  })
})
