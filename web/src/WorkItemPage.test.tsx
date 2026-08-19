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
    ID: 'work-1', Definition: { ID: 'blackboard-1', Version: 1, Mode: 'blackboard' }, Status: 'open',
    Title: 'Article', Goal: 'Publish a reviewed article', Context: '', Constraints: '', AcceptanceCriteria: '', Tags: [], Result: '',
    Version: 1, CreatedAt: '2026-08-19T08:00:00Z', UpdatedAt: '2026-08-19T08:00:00Z', CompletedAt: null,
    ...overrides,
  }
}

function completedTask(): Task {
  return {
    ID: 'task-1', WorkItemID: 'work-1', Status: 'completed', ActiveClaimID: null, ParentTaskID: null,
    Title: 'Draft article', Description: '', AcceptanceCriteria: '', Executor: 'agent', AllowedRoles: [], Tags: [],
    Reviews: [], Submissions: [], Failures: [], TransitionDecisions: [], Position: 0,
    CreatedAt: '2026-08-19T08:00:00Z', UpdatedAt: '2026-08-19T09:00:00Z', CompletedAt: '2026-08-19T09:00:00Z',
  }
}

function context(item: WorkItem, tasks: Task[] = []): WorkItemContext {
  return {
    WorkItem: item,
    Definition: { Name: 'Article collaboration', Description: '', AgentInstructions: '', SuggestedTags: [] },
    Tasks: tasks, Relations: [], Claims: [], ActiveClaims: [],
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
      Status: 'awaiting_human_acceptance', Result: 'Reviewed article ready for publication.', AcceptanceMode: 'human',
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
