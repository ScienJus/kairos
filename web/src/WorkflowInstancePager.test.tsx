// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from './i18n'
import type { Task } from './types'
import { WorkflowInstancePager } from './WorkItemPage'

function task(id: string, position: number): Task {
  return {
    id: id, work_item_id: 'work-1', workflow_task_id: 'write', status: 'completed', active_claim_id: null, parent_task_id: null,
    workflow_activation_id: 'activation-1', decomposed_at: null,
    title: 'Write', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: ['writer'], tags: [],
    reviews: [], submissions: [], failures: [], transition_decisions: [], position: position,
    created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z', completed_at: '2026-08-20T00:00:00Z',
    skipped_by: null, skip_reason: '', execution: 'required', review_policy: 'none', version: 1,
  }
}

beforeEach(() => localStorage.setItem('kairos-console-locale', 'en'))
afterEach(() => { cleanup(); localStorage.clear() })

describe('WorkflowInstancePager', () => {
  it('moves between concrete runtime Tasks without wrapping', async () => {
    const onSelect = vi.fn()
    const tasks = [task('write-1', 0), task('write-2', 2), task('write-3', 4)]
    const user = userEvent.setup()
    render(<I18nProvider><WorkflowInstancePager tasks={tasks} selectedTaskID="write-2" onSelect={onSelect} /></I18nProvider>)

    expect(screen.getByText('Instance 2 of 3')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Previous instance' }))
    await user.click(screen.getByRole('button', { name: 'Next instance' }))
    expect(onSelect.mock.calls).toEqual([['write-1'], ['write-3']])
  })

  it('does not render navigation for a node with one runtime Task', () => {
    const { container } = render(<I18nProvider><WorkflowInstancePager tasks={[task('write-1', 0)]} selectedTaskID="write-1" onSelect={vi.fn()} /></I18nProvider>)
    expect(container).toBeEmptyDOMElement()
  })
})
