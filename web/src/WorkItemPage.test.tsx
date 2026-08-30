// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { I18nProvider } from './i18n'
import { WorkItemPage } from './WorkItemPage'
import type { Identity, Task, WorkItem, WorkItemContext } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function workItem(overrides: Partial<WorkItem> = {}): WorkItem {
  return {
    id: 'work-1', definition: { id: 'blackboard-1', version: 1, mode: 'blackboard' }, status: 'open', acceptance_mode: 'none',
    title: 'Article', goal: 'Publish a reviewed article', context: '', constraints: '', acceptance_criteria: '', tags: [], result: '',
    version: 1, created_at: '2026-08-19T08:00:00Z', updated_at: '2026-08-19T08:00:00Z', completed_at: null,
    cancelled_at: null, cancelled_by: null, cancellation_reason: '',
    ...overrides,
  }
}

function completedTask(): Task {
  return {
    id: 'task-1', work_item_id: 'work-1', status: 'completed', active_claim_id: null, parent_task_id: null,
    workflow_task_id: null, workflow_activation_id: null, decomposed_at: null,
    title: 'Draft article', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], tags: [],
    reviews: [], submissions: [], failures: [], transition_decisions: [], position: 0,
    created_at: '2026-08-19T08:00:00Z', updated_at: '2026-08-19T09:00:00Z', completed_at: '2026-08-19T09:00:00Z',
    skipped_by: null, skip_reason: '', execution: null, review_policy: null, version: 1,
  }
}

function context(item: WorkItem, tasks: Task[] = []): WorkItemContext {
  return {
    work_item: item,
    definition: { name: 'Article collaboration', description: '', agent_instructions: '', suggested_tags: [] },
    tasks: tasks, relations: [], claims: [], active_claims: [], coordination_claims: [], active_coordination_claim: null, artifacts: [],
  }
}

function renderPage(actor: Identity = identity) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><WorkItemPage identity={actor} workItemID="work-1" selectedTaskID={null} homeView="human" navigate={vi.fn()} /></I18nProvider></QueryClientProvider>)
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('WorkItem lifecycle actions', () => {
  it('submits a converged Blackboard result and preserves it when submission fails', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem(), [completedTask()]))
    const submit = vi.spyOn(api, 'submitBlackboardCompletion').mockRejectedValue(new Error('Completion could not be submitted'))
    const user = userEvent.setup()
    renderPage()

    const result = await screen.findByRole('textbox', { name: 'Closing note' })
    await user.type(result, 'The article is ready for publication.')
    await user.click(screen.getByRole('button', { name: 'Submit completion' }))

    await waitFor(() => expect(submit).toHaveBeenCalledWith(identity, 'work-1', 'The article is ready for publication.'))
    expect(await screen.findByText('Completion could not be submitted')).toBeInTheDocument()
    expect(result).toHaveValue('The article is ready for publication.')
  })

  it('shows the proposed result and reports a human acceptance failure', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({
      status: 'awaiting_human_acceptance', result: 'Reviewed article ready for publication.', acceptance_mode: 'human',
    })))
    const accept = vi.spyOn(api, 'acceptBlackboardCompletion').mockRejectedValue(new Error('Acceptance could not be recorded'))
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Reviewed article ready for publication.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Accept and complete' }))

    await waitFor(() => expect(accept).toHaveBeenCalledWith(identity, 'work-1'))
    expect(await screen.findByText('Acceptance could not be recorded')).toBeInTheDocument()
  })

  it('requires a reason and cancels an active WorkItem as a human', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    const cancel = vi.spyOn(api, 'cancelWorkItem').mockResolvedValue(workItem({
      status: 'cancelled', cancelled_at: '2026-08-19T10:00:00Z', cancelled_by: { kind: 'human', id: 'human-1' }, cancellation_reason: 'No longer required',
    }))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Cancel WorkItem' }))
    const confirm = screen.getAllByRole('button', { name: 'Cancel WorkItem' }).at(-1)!
    expect(confirm).toBeDisabled()
    const reason = screen.getByRole('textbox', { name: 'Cancellation reason' })
    await user.type(reason, 'No longer required')
    await user.click(confirm)

    await waitFor(() => expect(cancel).toHaveBeenCalledWith(identity, 'work-1', 'No longer required'))
  })

  it('keeps the cancellation reason when the request fails', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    vi.spyOn(api, 'cancelWorkItem').mockRejectedValue(new Error('Cancellation could not be recorded'))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Cancel WorkItem' }))
    const reason = screen.getByRole('textbox', { name: 'Cancellation reason' })
    await user.type(reason, 'Duplicate request')
    await user.click(screen.getAllByRole('button', { name: 'Cancel WorkItem' }).at(-1)!)

    expect(await screen.findByText('Cancellation could not be recorded')).toBeInTheDocument()
    expect(reason).toHaveValue('Duplicate request')
  })

  it('hides management cancellation from agents and shows terminal cancellation details', async () => {
    const cancelled = workItem({
      status: 'cancelled', cancelled_at: '2026-08-19T10:00:00Z', cancelled_by: { kind: 'human', id: 'operator' }, cancellation_reason: 'The request was superseded.',
    })
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(cancelled))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('This WorkItem was cancelled')).toBeInTheDocument()
    expect(screen.getByText('The request was superseded.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Cancel WorkItem' })).not.toBeInTheDocument()
  })

  it('hides empty Blackboard lifecycle controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Article')).toBeInTheDocument()
    expect(screen.queryByText('How should this work begin?')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add task' })).not.toBeInTheDocument()
    expect(screen.queryByText('Nothing needs to be done')).not.toBeInTheDocument()
  })

  it('hides converged Blackboard lifecycle controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem(), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Draft article')).toBeInTheDocument()
    expect(screen.queryByText('Current plan is complete')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add task' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Submit completion' })).not.toBeInTheDocument()
  })

  it('does not offer WorkItem acceptance controls to agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({
      status: 'awaiting_agent_acceptance', result: 'Ready for acceptance.', acceptance_mode: 'agent',
    }), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Awaiting agent acceptance')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept and complete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Submit completion' })).not.toBeInTheDocument()
  })

  it('hides human WorkItem acceptance controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({
      status: 'awaiting_human_acceptance', result: 'Ready for human acceptance.', acceptance_mode: 'human',
    }), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Awaiting human acceptance')).toBeInTheDocument()
    expect(screen.queryByText('Work item acceptance')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept and complete' })).not.toBeInTheDocument()
  })
})
