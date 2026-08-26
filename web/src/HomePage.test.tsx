// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { HomePage } from './HomePage'
import { I18nProvider } from './i18n'
import type { HumanAttentionItem, Identity, WorkItem } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }
const activeStatuses: WorkItem['status'][] = ['open', 'awaiting_agent_acceptance', 'awaiting_human_acceptance']
const settledStatuses: WorkItem['status'][] = ['completed', 'cancelled', 'failed']

function workItem(id: string, status: WorkItem['status']): WorkItem {
  return {
    id, definition: { id: 'work', version: 1, mode: 'blackboard' }, status, acceptance_mode: 'none',
    title: id === 'active-work' ? 'Active work' : 'Completed work', goal: 'Keep the queues separate', context: '', constraints: '',
    acceptance_criteria: '', tags: [], result: '', version: 1, created_at: '2026-08-25T10:00:00Z', updated_at: '2026-08-25T11:00:00Z',
    completed_at: status === 'completed' ? '2026-08-25T11:00:00Z' : null, cancelled_at: null, cancelled_by: null, cancellation_reason: '',
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
