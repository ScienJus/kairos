// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { CreateWorkModal } from './AppModals'
import { I18nProvider } from './i18n'
import type { Identity } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

describe('Create Work modal', () => {
  it('loads the latest version for each Definition ID', async () => {
    const blackboards = vi.spyOn(api, 'listBlackboardDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    const workflows = vi.spyOn(api, 'listWorkflowDefinitions').mockResolvedValue({ data: [], next_cursor: null })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(<QueryClientProvider client={client}><I18nProvider><CreateWorkModal open onOpenChange={vi.fn()} identity={identity} /></I18nProvider></QueryClientProvider>)

    await waitFor(() => {
      expect(blackboards).toHaveBeenCalledWith(identity, undefined)
      expect(workflows).toHaveBeenCalledWith(identity, undefined)
    })
  })
})
