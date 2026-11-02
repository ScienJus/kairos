// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { CreateWorkModal } from './AppModals'
import { I18nProvider } from './i18n'
import type { Identity } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

function renderModal(client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  const navigate = vi.fn()
  const onOpenChange = vi.fn()
  render(<QueryClientProvider client={client}><I18nProvider><CreateWorkModal open onOpenChange={onOpenChange} identity={identity} navigate={navigate} /></I18nProvider></QueryClientProvider>)
  return { navigate, onOpenChange }
}

describe('Create Work modal', () => {
  it('loads the latest version for each Definition ID', async () => {
    const blackboards = vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    const workflows = vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    renderModal()

    await waitFor(() => {
      expect(blackboards).toHaveBeenCalledWith(identity, undefined)
      expect(workflows).toHaveBeenCalledWith(identity, undefined)
    })
  })

  it.each([
    ['New workflow', { workflowID: null, workflowEditing: true }],
    ['Open Blackboard library', { blackboardID: null }],
  ])('guides an empty workspace to %s', async (label, destination) => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    const { navigate, onOpenChange } = renderModal()
    fireEvent.click(await screen.findByRole('button', { name: label }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(navigate).toHaveBeenCalledWith({ workItemID: null, taskID: null, homeView: 'all', ...destination })
    expect(screen.queryByRole('button', { name: 'Open work' })).not.toBeInTheDocument()
  })

  it('does not mistake a pending definition request for a new workspace', () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    vi.spyOn(api, 'listWorkflowDefinitions').mockReturnValue(new Promise(() => {}))
    renderModal()
    expect(screen.getByRole('status')).toHaveTextContent('Loading collaboration plans')
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()
  })

  it.each(['empty', 'existing'])('waits for both cached empty lists to refresh before showing %s results', async result => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    for (const kind of ['blackboard-definitions', 'workflow-definitions']) {
      client.setQueryData([kind, identity], { pages: [{ data: [], next_cursor: null }], pageParams: [undefined] }, { updatedAt: 1 })
    }
    type Blackboards = Awaited<ReturnType<typeof api.listBlackboardDefinitions>>
    type Workflows = Awaited<ReturnType<typeof api.listWorkflowDefinitions>>
    let resolveBlackboards!: (page: Blackboards) => void
    let resolveWorkflows!: (page: Workflows) => void
    const blackboards = new Promise<Blackboards>(resolve => { resolveBlackboards = resolve })
    const workflows = new Promise<Workflows>(resolve => { resolveWorkflows = resolve })
    vi.spyOn(api, 'listBlackboardDefinitions').mockReturnValue(blackboards)
    vi.spyOn(api, 'listWorkflowDefinitions').mockReturnValue(workflows)
    renderModal(client)

    expect(screen.getByRole('status')).toHaveTextContent('Loading collaboration plans')
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()

    const data = result === 'empty' ? [] : [{ id: 'plan', version: 1, name: 'Plan', description: '', agent_instructions: '', suggested_tags: [] }]
    await act(async () => { resolveBlackboards({ data, next_cursor: null }); await blackboards })
    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()

    await act(async () => { resolveWorkflows({ data: [], next_cursor: null }); await workflows })
    if (result === 'empty') {
      expect(await screen.findByRole('button', { name: 'New workflow' })).toBeInTheDocument()
    } else {
      expect(await screen.findByRole('option', { name: 'Plan · v1 · Blackboard' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Open work' })).toBeEnabled()
      expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()
    }
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
  })

  it('shows a loading error instead of onboarding', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    vi.spyOn(api, 'listWorkflowDefinitions').mockRejectedValue(new Error('Cannot load workflows'))
    renderModal()
    expect(await screen.findByText('Cannot load workflows')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()
  })

  it('keeps the creation form available when either kind has a definition', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [{ id: 'plan', version: 1, name: 'Plan', description: '', agent_instructions: '', suggested_tags: [] }], next_cursor: null })
    vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    renderModal()
    expect(await screen.findByRole('option', { name: 'Plan · v1 · Blackboard' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open work' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()
  })

  it('allows another page to be loaded before deciding the library is empty', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: 'next-page' })
    vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    renderModal()
    expect(await screen.findByRole('button', { name: 'Load more' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'New workflow' })).not.toBeInTheDocument()
  })
})
