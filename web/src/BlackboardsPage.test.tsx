// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { BlackboardsPage } from './BlackboardsPage'
import { I18nProvider } from './i18n'
import type { Definition, Identity } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function definition(version: number, name = 'Product work'): Definition {
  return {
    id: 'product-work', version: version, name: name, description: `Description v${version}`,
    agent_instructions: `Plan version ${version}`, suggested_tags: ['product'], status: 'published',
  }
}

function renderPage(navigate = vi.fn(), version = 2, onStartWork = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(<QueryClientProvider client={client}><I18nProvider><BlackboardsPage identity={identity} blackboardID="product-work" blackboardVersion={version} navigate={navigate} onStartWork={onStartWork} /></I18nProvider></QueryClientProvider>)
  return { navigate, onStartWork }
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('Blackboard definition library', () => {
  it('groups immutable versions under one shelf entry', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue([definition(1), definition(2)])
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Product work' })).toBeInTheDocument()
    expect(screen.getAllByText('Description v2')).toHaveLength(2)
    expect(screen.getByRole('button', { name: /v2\s*Published/ })).toHaveClass('selected')
    expect(screen.getByRole('button', { name: /v1\s*Published/ })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /Product work/ })).toHaveLength(1)
  })

  it('publishes edits as the next immutable version', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue([definition(1), definition(2)])
    const created = definition(3, 'Product delivery')
    const create = vi.spyOn(api, 'createDefinition').mockResolvedValue(created)
    const { navigate } = renderPage()
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Create next version' }))
    const name = screen.getByRole('textbox', { name: 'Display name' })
    await user.clear(name)
    await user.type(name, 'Product delivery')
    await user.click(screen.getByRole('button', { name: 'Publish new version' }))

    await waitFor(() => expect(create).toHaveBeenCalledWith(identity, expect.objectContaining({
      id: 'product-work', version: 3, name: 'Product delivery', status: 'published',
    })))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ blackboardID: 'product-work', blackboardVersion: 3 })))
  })

  it('keeps historical versions read-only', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue([definition(1), definition(2)])
    renderPage(vi.fn(), 1)

    expect(await screen.findByText('Description v1')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Create next version' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Use this blackboard/ })).not.toBeInTheDocument()
  })

  it('starts work with the selected latest Blackboard', async () => {
    vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue([definition(1), definition(2)])
    const onStartWork = vi.fn()
    renderPage(vi.fn(), 2, onStartWork)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: /Use this blackboard/ }))

    expect(onStartWork).toHaveBeenCalledWith({ id: 'product-work', mode: 'blackboard', name: 'Product work', version: 2 })
  })
})
