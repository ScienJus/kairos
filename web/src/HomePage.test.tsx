// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { HomePage } from './HomePage'
import { I18nProvider } from './i18n'
import type { HumanAttentionItem, Identity, Task, WorkItem } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }
const activeStatuses: WorkItem['status'][] = ['open', 'awaiting_agent_acceptance', 'awaiting_human_acceptance']
const settledStatuses: WorkItem['status'][] = ['completed', 'cancelled', 'failed']

function workItem(id: string, status: WorkItem['status']): WorkItem {
  return {
    id, definition: { id: 'work', version: 1, mode: 'blackboard' }, status, acceptance_mode: 'none',
    title: id === 'active-work' ? 'Active work' : 'Completed work', goal: 'Keep the queues separate', context: '', constraints: '',
    acceptance_criteria: '', tags: [], result: '', version: 1, created_at: '2026-08-25T10:00:00Z', updated_at: '2026-08-25T11:00:00Z',
    completed_at: status === 'completed' ? '2026-08-25T11:00:00Z' : null, cancelled_at: null, cancelled_by: null, cancellation_reason: '', started_over_from_work_item_id: null, start_over_context: '', recovery_instructions: '', failure: null, workflow_max_task_instances_per_node: 0,
  }
}

function attentionItem(id: string): HumanAttentionItem {
  return {
    kind: 'work_item_acceptance',
    work_item: {
      ...workItem(id, 'awaiting_human_acceptance'),
      title: id === 'attention-one' ? 'First approval' : 'Second approval',
    },
    task: null,
  }
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('Home WorkItem pagination', () => {
  it('loads active work and settled history through independent status cursors', async () => {
    const list = vi.spyOn(api, 'listWorkItems').mockImplementation(async (_identity, cursor, options) => {
      if (options?.statuses?.includes('open')) {
        return cursor ? { data: [], next_cursor: null } : { data: [workItem('active-work', 'open')], next_cursor: 'active-next' }
      }
      return { data: [workItem('settled-work', 'completed')], next_cursor: 'settled-next' }
    })
    vi.spyOn(api, 'listHumanAttention').mockResolvedValue({ data: [], next_cursor: null })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><I18nProvider><HomePage identity={identity} homeView="all" navigate={vi.fn()} onCreate={vi.fn()} /></I18nProvider></QueryClientProvider>)

    expect(await screen.findByText('Active work')).toBeInTheDocument()
    expect(await screen.findByText('Completed work')).toBeInTheDocument()
    expect(list).toHaveBeenCalledWith(identity, undefined, { statuses: activeStatuses })
    expect(list).toHaveBeenCalledWith(identity, undefined, { statuses: settledStatuses })

    await userEvent.click(screen.getByRole('button', { name: 'Load more active work' }))

    await waitFor(() => expect(list).toHaveBeenCalledWith(identity, 'active-next', { statuses: activeStatuses }))
    expect(screen.getByRole('button', { name: 'Load more history' })).toBeInTheDocument()
  })

  it('loads Human Attention through its own cursor', async () => {
    vi.spyOn(api, 'listWorkItems').mockResolvedValue({ data: [], next_cursor: null })
    const listAttention = vi.spyOn(api, 'listHumanAttention').mockImplementation(async (_identity, cursor) => (
      cursor
        ? { data: [attentionItem('attention-two')], next_cursor: null }
        : { data: [attentionItem('attention-one')], next_cursor: 'attention-next' }
    ))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><I18nProvider><HomePage identity={identity} homeView="human" navigate={vi.fn()} onCreate={vi.fn()} /></I18nProvider></QueryClientProvider>)

    expect(await screen.findByText('First approval')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Needs a person' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Load more' }))

    expect(await screen.findByText('Second approval')).toBeInTheDocument()
    expect(listAttention).toHaveBeenCalledWith(identity, 'attention-next')
  })
})

describe('Workflow recovery attention', () => {
  const task: Task = {
    id: 'failed-task', work_item_id: 'recovery', parent_task_id: null, workflow_task_id: 'writer', workflow_activation_id: null,
    retry_of_task_id: null, retry_context: '', retry_instructions: '', decomposed_at: null,
    title: 'Writer', status: 'failed', executor: 'agent', active_claim_id: null, allowed_roles: [], tags: [],
    reviews: [], submissions: [], transition_decisions: [], version: 1, position: 0,
    description: '', acceptance_criteria: '', skipped_by: null, skip_reason: '', execution: 'required', review_policy: 'none',
    created_at: '2026-10-18T10:00:00Z', updated_at: '2026-10-18T11:00:00Z', completed_at: null,
    failures: [{ id: 'failure', task_id: 'failed-task', claim_id: 'claim', action: 'await_human', reason: 'Missing permission', retry_prompt: '', failed_at: '2026-10-18T11:00:00Z' }],
  }

  it.each([
    { locale: 'en', status: 'open' as const, failedTask: task, failure: null, reason: 'Missing permission', label: 'Needs recovery' },
    { locale: 'en', status: 'failed' as const, failedTask: null, failure: { kind: 'workflow_task_instance_limit' as const, message: 'Writer reached the execution limit', workflow_task_id: 'writer', task_instances: 10, limit: 10 }, reason: 'Writer reached the execution limit', label: 'Needs recovery' },
    { locale: 'zh-CN', status: 'failed' as const, failedTask: task, failure: { kind: 'execution_failure' as const, message: '工作失败，需要处理', workflow_task_id: '', task_instances: 0, limit: 0 }, reason: '工作失败，需要处理', label: '需要恢复' },
  ])('shows the recovery reason and opens the WorkItem ($locale, $status)', async ({ locale, status, failedTask, failure, reason, label }) => {
    localStorage.setItem('kairos-console-locale', locale)
    vi.spyOn(api, 'listWorkItems').mockResolvedValue({ data: [], next_cursor: null })
    vi.spyOn(api, 'listHumanAttention').mockResolvedValue({ data: [{
      kind: 'workflow_recovery', task: failedTask,
      work_item: { ...workItem('recovery', status), title: 'Delivery workflow', definition: { id: 'delivery', version: 1, mode: 'workflow' }, failure },
    }], next_cursor: null })
    const navigate = vi.fn()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><I18nProvider><HomePage identity={identity} homeView="human" navigate={navigate} onCreate={vi.fn()} /></I18nProvider></QueryClientProvider>)

    const entry = await screen.findByRole('button', { name: /Delivery workflow/ })
    expect(entry).toHaveTextContent(reason)
    expect(screen.getAllByText(label)).toHaveLength(1)
    await userEvent.click(entry)
    expect(navigate).toHaveBeenCalledWith({ workItemID: 'recovery', taskID: null, homeView: 'human' })
  })
})
