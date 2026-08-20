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
    ID: id, WorkItemID: 'work-1', WorkflowTaskID: 'write', Status: 'completed', ActiveClaimID: null, ParentTaskID: null,
    Title: 'Write', Description: '', AcceptanceCriteria: '', Executor: 'agent', AllowedRoles: ['writer'], Tags: [],
    Reviews: [], Submissions: [], Failures: [], TransitionDecisions: [], Position: position,
    CreatedAt: '2026-08-20T00:00:00Z', UpdatedAt: '2026-08-20T00:00:00Z', CompletedAt: '2026-08-20T00:00:00Z',
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
