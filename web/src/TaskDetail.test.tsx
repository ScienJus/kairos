// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { I18nProvider } from './i18n'
import { TaskDetail } from './TaskDetail'
import type { Claim, Identity, Task, TaskDetailView, TaskExecutionContext, WorkItem } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }
const claim: Claim = {
  ID: 'claim-1', TaskID: 'task-1', Executor: { Kind: 'human', ID: identity.id },
  ClaimedAt: '2026-08-17T08:00:00Z', EndedAt: null, EndReason: '',
}

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    ID: 'task-1', WorkItemID: 'work-1', Status: 'working', ActiveClaimID: claim.ID, ParentTaskID: null,
    Title: 'Prepare release', Description: 'Prepare the release notes.', AcceptanceCriteria: 'Notes are ready.',
    Executor: 'human', AllowedRoles: [], Tags: [], Reviews: [], Submissions: [], Failures: [],
    TransitionDecisions: [], Position: 0, CreatedAt: '2026-08-17T08:00:00Z',
    UpdatedAt: '2026-08-17T08:00:00Z', CompletedAt: null, ReviewPolicy: 'executor_decides',
    ...overrides,
  }
}

function makeWorkItem(): WorkItem {
  return {
    ID: 'work-1', Definition: { ID: 'definition-1', Version: 1, Mode: 'blackboard' }, Status: 'open',
    Title: 'Release', Goal: 'Ship safely', Context: '', Constraints: '', AcceptanceCriteria: '', Tags: [], Result: '',
    Version: 1, CreatedAt: '2026-08-17T08:00:00Z', UpdatedAt: '2026-08-17T08:00:00Z', CompletedAt: null,
  }
}

function execution(task: Task, claims: Claim[] = []): TaskExecutionContext {
  const actor = task.SkippedBy ?? claims[0]?.Executor ?? null
  const kind = task.Status === 'skipped' ? 'skipped_by' : task.Status === 'completed' ? 'executed_by' : task.Status === 'working' ? 'claimed_by' : 'unclaimed'
  return { WorkItem: makeWorkItem(), Task: task, Claims: claims, Responsibility: { Kind: kind, Actor: actor }, Outcome: { Kind: task.Status, Actor: actor, Reason: task.SkipReason }, Workflow: null, Blackboard: { Tasks: [task], Relations: [], CanDecompose: false } }
}

function detail(task: Task, claims: Claim[] = []): TaskDetailView {
  const actor = task.SkippedBy ?? claims[0]?.Executor ?? null
  const kind = task.Status === 'skipped' ? 'skipped_by' : task.Status === 'completed' ? 'executed_by' : task.Status === 'working' ? 'claimed_by' : 'unclaimed'
  const ownsClaim = claims.some(item => !item.EndedAt && item.Executor.Kind === identity.kind && item.Executor.ID === identity.id)
  const canExecute = task.Executor === 'human' || task.Executor === 'either'
  return {
    Task: task, Responsibility: { Kind: kind, Actor: actor }, Outcome: { Kind: task.Status, Actor: actor, Reason: task.SkipReason }, CurrentReview: task.Reviews.at(-1) ?? null,
    History: { Claims: claims, Submissions: task.Submissions, Reviews: task.Reviews, Failures: task.Failures, TransitionDecisions: task.TransitionDecisions },
    Capabilities: { CanClaim: task.Status === 'pending' && canExecute, CanSubmit: task.Status === 'working' && ownsClaim, CanRelease: task.Status === 'working' && ownsClaim, CanFail: task.Status === 'working' && ownsClaim, CanReview: task.Status === 'in_review', CanSkip: task.Status === 'pending', CanDecompose: false, CanAddChild: task.Status === 'waiting_children' },
  }
}

function renderTask(task: Task, activeClaim: Claim | null = null, mode = 'blackboard', executionClaim: Claim | null = null) {
  vi.spyOn(api, 'getTaskDetail').mockResolvedValue(detail(task, activeClaim ? [activeClaim] : executionClaim ? [executionClaim] : []))
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}><I18nProvider><TaskDetail task={task} activeClaim={activeClaim} executionClaim={executionClaim} identity={identity} mode={mode} /></I18nProvider></QueryClientProvider>)
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
  it('shows the executor from the completed task submission claim', async () => {
    const endedClaim = { ...claim, Executor: { Kind: 'agent' as const, ID: 'codex-backend' }, EndedAt: '2026-08-17T09:00:00Z', EndReason: 'submitted' }
    const task = makeTask({
      Status: 'completed', ActiveClaimID: null, CompletedAt: '2026-08-17T09:00:00Z',
      Submissions: [{ ID: 'submission-1', TaskID: 'task-1', ClaimID: endedClaim.ID, Result: 'Done', SubmittedAt: '2026-08-17T09:00:00Z' }],
    })
    const getExecutionContext = vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task, [endedClaim]))

    renderTask(task, null, 'blackboard', endedClaim)

    expect(await screen.findByText('Executed by')).toBeInTheDocument()
    expect(await screen.findByText('codex-backend')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
    expect(getExecutionContext).not.toHaveBeenCalled()
  })

  it('shows the skip decision instead of an unclaimed executor', async () => {
    const task = makeTask({ Status: 'skipped', ActiveClaimID: null, CompletedAt: '2026-08-17T09:00:00Z', SkippedBy: { Kind: 'agent', ID: 'planner-1' }, SkipReason: 'Covered by another task' })
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(task))

    renderTask(task)

    expect(await screen.findByText('Skipped by')).toBeInTheDocument()
    expect(await screen.findByText('planner-1')).toBeInTheDocument()
    expect(await screen.findByText('Covered by another task')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
  })

  it('uses lifecycle-specific responsibility labels and does not call a missing skip actor unclaimed', async () => {
    const task = makeTask({ Status: 'skipped', ActiveClaimID: null, SkippedBy: null })
    renderTask(task)

    expect(await screen.findByText('Skipped by')).toBeInTheDocument()
    expect(screen.getByText('Not recorded')).toBeInTheDocument()
    expect(screen.queryByText('Unclaimed')).not.toBeInTheDocument()
  })

  it('renders safely when optional task collections are empty', async () => {
    const task = makeTask({ Status: 'pending', ActiveClaimID: null, Reviews: [], Tags: [], Submissions: [], Failures: [] })
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

    await waitFor(() => expect(fail).toHaveBeenCalledWith(identity, task.ID, {
      claim_id: claim.ID, action: 'fail_work_item', reason: 'The requested outcome is impossible', retry_prompt: '',
    }))
  })

  it('opens review and Blackboard planning actions for their matching states', async () => {
    const reviewTask = makeTask({
      Status: 'in_review', ActiveClaimID: null,
      Reviews: [{ ID: 'review-1', TaskID: 'task-1', SubmissionID: null, Status: 'pending', RequestedBy: 'agent-1', RequestedAt: '2026-08-17T08:00:00Z', DecidedBy: null, DecidedAt: null, Feedback: '' }],
    })
    vi.spyOn(api, 'getTaskContext').mockResolvedValue(execution(reviewTask))
    const { unmount } = renderTask(reviewTask)
    expect(await screen.findByRole('button', { name: 'Review this result' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled()
    unmount()

    const agentTask = makeTask({ Status: 'pending', ActiveClaimID: null, Executor: 'agent' })
    renderTask(agentTask)
    expect(await screen.findByRole('button', { name: 'This is no longer needed' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('textbox', { name: 'Why is it no longer needed?' })).toBeInTheDocument()
  })
})
