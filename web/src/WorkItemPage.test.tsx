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
    tasks: tasks, relations: [], claims: [], active_claims: [], artifacts: [],
  }
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><WorkItemPage identity={identity} workItemID="work-1" selectedTaskID={null} homeView="human" navigate={vi.fn()} /></I18nProvider></QueryClientProvider>)
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
})
