// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APIError, api } from './api'
import { I18nProvider } from './i18n'
import type { Identity, WorkflowDefinition } from './types'
import { WorkflowEditorPage } from './WorkflowEditorPage'

vi.mock('./WorkflowEditorMap', () => ({ WorkflowEditorMap: () => <div data-testid="workflow-editor-map" /> }))

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function definition(version: number): WorkflowDefinition {
  return {
    id: 'delivery', version, name: `Delivery v${version}`, description: '', agent_instructions: '', suggested_tags: [],
    graph: {
      start_task_ids: ['implement'], max_task_executions: 20, relations: [],
      tasks: [{
        id: 'implement', title: 'Implement', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [],
        execution: 'required', review_policy: 'none', default_tags: [], artifacts: [],
      }],
    },
  }
}

function renderPage(version: number, navigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(<QueryClientProvider client={client}><I18nProvider><WorkflowEditorPage identity={identity} workflowID="delivery" workflowVersion={version} navigate={navigate} /></I18nProvider></QueryClientProvider>)
  return navigate
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('Workflow version editor', () => {
  it('initializes the next draft only from the latest version', async () => {
    vi.spyOn(api, 'getWorkflowDefinition').mockResolvedValue(definition(2))
		const latest = vi.spyOn(api, 'getLatestWorkflowDefinition').mockResolvedValue(definition(2))

    renderPage(2)

    expect(await screen.findByRole('button', { name: 'Create v3' })).toBeInTheDocument()
    expect(screen.getByTestId('workflow-editor-map')).toBeInTheDocument()
		expect(latest).toHaveBeenCalledWith(identity, 'delivery')
  })

  it('keeps a historical version read-only even when addressed directly', async () => {
    vi.spyOn(api, 'getWorkflowDefinition').mockResolvedValue(definition(1))
		vi.spyOn(api, 'getLatestWorkflowDefinition').mockResolvedValue(definition(2))
    const create = vi.spyOn(api, 'createWorkflowDefinition')

    const navigate = renderPage(1)

    expect(await screen.findByText('Only the latest Workflow version can be edited.')).toBeInTheDocument()
    expect(screen.queryByTestId('workflow-editor-map')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Create v/ })).not.toBeInTheDocument()
    expect(create).not.toHaveBeenCalled()
    expect(localStorage.getItem('kairos-workflow-draft:delivery:v1')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ workflowID: 'delivery', workflowVersion: 2 }))
  })

  it('reports an exact-version load failure', async () => {
    vi.spyOn(api, 'getWorkflowDefinition').mockRejectedValue(new APIError(404, 'Workflow version not found'))
		vi.spyOn(api, 'getLatestWorkflowDefinition').mockResolvedValue(definition(2))

    renderPage(1)

    await waitFor(() => expect(screen.getByText('Workflow version not found')).toBeInTheDocument())
    expect(screen.queryByTestId('workflow-editor-map')).not.toBeInTheDocument()
  })
})
