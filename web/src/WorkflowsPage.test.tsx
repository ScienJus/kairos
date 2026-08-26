// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APIError, api } from './api'
import { I18nProvider } from './i18n'
import type { Identity, WorkflowDefinition } from './types'
import { WorkflowsPage } from './WorkflowsPage'

vi.mock('./WorkflowDefinitionMap', () => ({ WorkflowDefinitionMap: () => <div data-testid="workflow-map" /> }))

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function definition(id: string, version: number): WorkflowDefinition {
  return {
    id, version, name: id === 'delivery' ? 'Delivery' : 'Operations', description: '', agent_instructions: '', suggested_tags: [],
    graph: { start_task_ids: ['implement'], relations: [], max_task_executions: 20, tasks: [{
      id: 'implement', title: 'Implement', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], execution: 'required', review_policy: 'none', default_tags: [], artifacts: [],
    }] },
  }
}

function renderPage(version = 2) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><I18nProvider><WorkflowsPage identity={identity} workflowID="delivery" workflowVersion={version} navigate={vi.fn()} onStartWork={vi.fn()} /></I18nProvider></QueryClientProvider>)
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('Workflow definition routes', () => {
  it('resolves a directly addressed version without consuming shelf pages', async () => {
		const list = vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [definition('operations', 1)], next_cursor: 'delivery-page' })
		const versions = vi.spyOn(api, 'listWorkflowDefinitionVersions').mockResolvedValue({ data: [definition('delivery', 2), definition('delivery', 1)], next_cursor: null })
    vi.spyOn(api, 'getWorkflowDefinition').mockResolvedValue(definition('delivery', 2))

    renderPage()

    expect(await screen.findByRole('heading', { name: 'Delivery' })).toBeInTheDocument()
    expect(screen.getByTestId('workflow-map')).toBeInTheDocument()
		expect(versions).toHaveBeenCalledWith(identity, 'delivery', undefined)
    expect(list).not.toHaveBeenCalledWith(identity, 'delivery-page')
  })

  it('shows an exact-version 404 without falling back to the latest version', async () => {
		const list = vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: 'delivery-page' })
		vi.spyOn(api, 'listWorkflowDefinitionVersions').mockResolvedValue({ data: [definition('delivery', 2)], next_cursor: null })
    vi.spyOn(api, 'getWorkflowDefinition').mockRejectedValue(new APIError(404, 'Workflow version not found'))

    renderPage(99)

    expect(await screen.findByText('Workflow version not found')).toBeInTheDocument()
    expect(screen.queryByTestId('workflow-map')).not.toBeInTheDocument()
    expect(list).not.toHaveBeenCalledWith(identity, 'delivery-page')
  })
})
