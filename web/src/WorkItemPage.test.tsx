// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { I18nProvider } from './i18n'
import { WorkItemPage } from './WorkItemPage'
import type { Identity, Task, WorkItem, WorkItemContext } from './types'

const identity: Identity = { id: 'human-1', kind: 'human', role: '' }

function workItem(overrides: Partial<WorkItem> = {}): WorkItem {
  return {
    id: 'work-1', definition: { id: 'blackboard-1', version: 1, mode: 'blackboard' }, status: 'open', acceptance_mode: 'none',
    title: 'Article', goal: 'Publish a reviewed article', context: '', constraints: '', acceptance_criteria: '', tags: [], result: '',
    version: 1, created_at: '2026-08-19T08:00:00Z', updated_at: '2026-08-19T08:00:00Z', completed_at: null,
    cancelled_at: null, cancelled_by: null, cancellation_reason: '', restart_of_work_item_id: null, restart_context: '', recovery_instructions: '', failure: null, workflow_max_task_executions: 0,
    ...overrides,
  }
}

function completedTask(): Task {
  return {
    id: 'task-1', work_item_id: 'work-1', status: 'completed', active_claim_id: null, parent_task_id: null,
    workflow_task_id: null, workflow_activation_id: null, retry_of_task_id: null, retry_context: '', retry_instructions: '', decomposed_at: null,
    title: 'Draft article', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], tags: [],
    reviews: [], submissions: [], failures: [], transition_decisions: [], position: 0,
    created_at: '2026-08-19T08:00:00Z', updated_at: '2026-08-19T09:00:00Z', completed_at: '2026-08-19T09:00:00Z',
    skipped_by: null, skip_reason: '', execution: null, review_policy: null, version: 1,
  }
}

function context(item: WorkItem, tasks: Task[] = []): WorkItemContext {
  return {
    work_item: item,
    recovery_task_ids: tasks.filter(task => task.status === 'failed' && !tasks.some(next => next.retry_of_task_id === task.id)).map(task => task.id),
    definition: { name: 'Article collaboration', description: '', agent_instructions: '', suggested_tags: [] },
    tasks: tasks, relations: [], claims: [], active_claims: [], coordination_claims: [], active_coordination_claim: null, artifacts: [],
  }
}

function renderPage(actor: Identity = identity, navigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><I18nProvider><WorkItemPage identity={actor} workItemID="work-1" selectedTaskID={null} homeView="human" navigate={navigate} /></I18nProvider></QueryClientProvider>)
}

beforeEach(() => {
  localStorage.setItem('kairos-console-locale', 'en')
  vi.stubGlobal('ResizeObserver', class { observe = vi.fn(); unobserve = vi.fn(); disconnect = vi.fn() })
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear() })

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
      status: 'awaiting_human_acceptance', result: 'Reviewed article ready for publication.', acceptance_mode: 'human',
    })))
    const accept = vi.spyOn(api, 'acceptBlackboardCompletion').mockRejectedValue(new Error('Acceptance could not be recorded'))
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Reviewed article ready for publication.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Accept and complete' }))

    await waitFor(() => expect(accept).toHaveBeenCalledWith(identity, 'work-1'))
    expect(await screen.findByText('Acceptance could not be recorded')).toBeInTheDocument()
  })

  it('requires a reason and cancels an active WorkItem as a human', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    const cancel = vi.spyOn(api, 'cancelWorkItem').mockResolvedValue(workItem({
      status: 'cancelled', cancelled_at: '2026-08-19T10:00:00Z', cancelled_by: { kind: 'human', id: 'human-1' }, cancellation_reason: 'No longer required',
    }))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Cancel WorkItem' }))
    const confirm = screen.getAllByRole('button', { name: 'Cancel WorkItem' }).at(-1)!
    expect(confirm).toBeDisabled()
    const reason = screen.getByRole('textbox', { name: 'Cancellation reason' })
    await user.type(reason, 'No longer required')
    await user.click(confirm)

    await waitFor(() => expect(cancel).toHaveBeenCalledWith(identity, 'work-1', 'No longer required'))
  })

  it('keeps the cancellation reason when the request fails', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    vi.spyOn(api, 'cancelWorkItem').mockRejectedValue(new Error('Cancellation could not be recorded'))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Cancel WorkItem' }))
    const reason = screen.getByRole('textbox', { name: 'Cancellation reason' })
    await user.type(reason, 'Duplicate request')
    await user.click(screen.getAllByRole('button', { name: 'Cancel WorkItem' }).at(-1)!)

    expect(await screen.findByText('Cancellation could not be recorded')).toBeInTheDocument()
    expect(reason).toHaveValue('Duplicate request')
  })

  it('hides management cancellation from agents and shows terminal cancellation details', async () => {
    const cancelled = workItem({
      status: 'cancelled', cancelled_at: '2026-08-19T10:00:00Z', cancelled_by: { kind: 'human', id: 'operator' }, cancellation_reason: 'The request was superseded.',
    })
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(cancelled))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('This WorkItem was cancelled')).toBeInTheDocument()
    expect(screen.getByText('The request was superseded.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Cancel WorkItem' })).not.toBeInTheDocument()
  })

  it('hides empty Blackboard lifecycle controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem()))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Article')).toBeInTheDocument()
    expect(screen.queryByText('How should this work begin?')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add task' })).not.toBeInTheDocument()
    expect(screen.queryByText('Nothing needs to be done')).not.toBeInTheDocument()
  })

  it('hides converged Blackboard lifecycle controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem(), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Draft article')).toBeInTheDocument()
    expect(screen.queryByText('Current plan is complete')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add task' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Submit completion' })).not.toBeInTheDocument()
  })

  it('does not offer WorkItem acceptance controls to agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({
      status: 'awaiting_agent_acceptance', result: 'Ready for acceptance.', acceptance_mode: 'agent',
    }), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Awaiting agent acceptance')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept and complete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Submit completion' })).not.toBeInTheDocument()
  })

  it('hides human WorkItem acceptance controls from agents', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({
      status: 'awaiting_human_acceptance', result: 'Ready for human acceptance.', acceptance_mode: 'human',
    }), [completedTask()]))
    renderPage({ id: 'agent-1', kind: 'agent', role: 'generalist' })

    expect(await screen.findByText('Awaiting human acceptance')).toBeInTheDocument()
    expect(screen.queryByText('Work item acceptance')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept and complete' })).not.toBeInTheDocument()
  })
})

function failedWorkflow(kind: 'workflow_execution_limit' | 'execution_failure' = 'workflow_execution_limit', count = 10) {
  const value = workItem({ definition: { id: 'delivery', version: 3, mode: 'workflow' }, status: 'failed', failure: { kind, message: 'Guard reached', workflow_task_id: kind === 'execution_failure' ? '' : 'dev', executions: kind === 'execution_failure' ? 0 : count, limit: kind === 'execution_failure' ? 0 : count } })
  vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(value))
  vi.spyOn(api, 'getWorkflowDefinition').mockResolvedValue({
    id: 'delivery', version: 3, name: 'Delivery', description: '', agent_instructions: '', suggested_tags: [],
    graph: { max_task_executions: count, start_task_ids: ['dev'], relations: [], tasks: [{ id: 'dev', title: 'Development', description: '', acceptance_criteria: '', executor: 'agent', allowed_roles: [], execution: 'required', review_policy: 'none', default_tags: [], artifacts: [] }] },
  })
  return value
}

describe('Workflow failure recovery', () => {
  it('explains the blocked node and retains input on a recovery error', async () => {
    failedWorkflow()
    const resume = vi.spyOn(api, 'resumeWorkflow').mockRejectedValue(new Error('Recovery could not be recorded'))
    renderPage()
    expect(await screen.findByText(/Node “Development” has created 10 task instances/)).toBeInTheDocument()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Continue execution' }))
    const input = screen.getByRole('spinbutton', { name: 'Max executions per node' })
    expect(input).toHaveValue(11)
    expect(input).toHaveAttribute('min', '11')
    await user.clear(input); await user.type(input, '10')
    expect(within(screen.getByRole('dialog')).getByRole('button', { name: 'Continue execution' })).toBeDisabled()
    await user.clear(input); await user.type(input, '20')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Continue execution' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith(identity, 'work-1', 1, 20, ''))
    expect(await screen.findByText('Recovery could not be recorded')).toBeInTheDocument()
    expect(input).toHaveValue(20)
  })

  it('allows ordinary Workflow failures to resume with the same per-node limit', async () => {
    const value = failedWorkflow('execution_failure')
    const resume = vi.spyOn(api, 'resumeWorkflow').mockResolvedValue({ ...value, status: 'open', failure: null })
    renderPage()
    expect(await screen.findByText('Guard reached')).toBeInTheDocument()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Continue execution' }))
    expect(screen.getByRole('spinbutton')).toHaveValue(10)
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Continue execution' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith(identity, 'work-1', 1, 10, ''))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows failure details but no recovery control to agents', async () => {
    failedWorkflow()
    renderPage({ id: 'developer', kind: 'agent', role: 'developer' })
    expect(await screen.findByText('A human must resume this Workflow from the console.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
  })

  it('explains when the maximum supported limit prevents recovery', async () => {
    failedWorkflow('workflow_execution_limit', 500)
    renderPage()
    expect(await screen.findByText(/supported maximum of 500/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
  })

  it('shows ordinary failure reasons without offering execution-limit recovery', async () => {
    vi.spyOn(api, 'getWorkItem').mockResolvedValue(context(workItem({ status: 'failed', failure: { kind: 'execution_failure', message: '部署配置缺失', workflow_task_id: '', executions: 0, limit: 0 } })))
    renderPage()
    expect(await screen.findByText('部署配置缺失')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
  })
})


describe('Workflow restart', () => {
  it('starts a new WorkItem with operator notes and navigates to it', async () => {
    const source = failedWorkflow('workflow_execution_limit', 500)
    const restart = vi.spyOn(api, 'restartWorkflow').mockResolvedValue({ ...source, id: 'new-work', status: 'open', failure: null, restart_of_work_item_id: source.id })
    const navigate = vi.fn()
    renderPage(identity, navigate)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Start over' }))
    expect(screen.getByText(/Scoped executors cannot read that history; list existing Issues, PRs/)).toBeInTheDocument()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: 'Additional instructions (optional)' }), 'Reuse the existing GitHub issue')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create new WorkItem' }))
    await waitFor(() => expect(restart).toHaveBeenCalledWith(identity, 'work-1', 1, 'Reuse the existing GitHub issue'))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ workItemID: 'new-work', taskID: null, homeView: 'human' }))
  })

  it('offers both actions for an ordinary Workflow failure', async () => {
    const source = failedWorkflow()
    vi.mocked(api.getWorkItem).mockResolvedValue(context({ ...source, failure: { kind: 'execution_failure', message: 'Missing configuration', workflow_task_id: '', executions: 0, limit: 0 } }))
    renderPage()
    expect(await screen.findByText('Missing configuration')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue execution' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start over' })).toBeInTheDocument()
  })
})

function failedTask(id: string, node = id): Task {
  return { ...completedTask(), id, workflow_task_id: node, workflow_activation_id: `activation-${id}`, status: 'failed', completed_at: null,
    failures: [{ id: `failure-${id}`, task_id: id, claim_id: `claim-${id}`, action: 'fail_task', reason: `${id} failed`, retry_prompt: '', failed_at: '2026-08-19T09:00:00Z' }] }
}

describe('Unified Continue execution', () => {
  it('continues all task-only failures from the WorkItem and ignores replaced history', async () => {
    const base = failedWorkflow()
    const value = { ...base, status: 'open' as const, failure: null }
    const a = failedTask('a'), b = failedTask('b')
    vi.mocked(api.getWorkItem).mockResolvedValue(context(value, [a, b]))
    const resumed = { ...value, version: 2 }
    const resume = vi.spyOn(api, 'resumeWorkflow').mockImplementation(async () => {
      vi.mocked(api.getWorkItem).mockResolvedValue(context(resumed, [a, b,
        { ...a, id: 'a2', status: 'pending', failures: [], retry_of_task_id: a.id },
        { ...b, id: 'b2', status: 'pending', failures: [], retry_of_task_id: b.id },
      ]))
      return resumed
    })
    renderPage()
    expect(await screen.findByText('Some tasks need attention')).toBeInTheDocument()
    expect(screen.getByText(/a failed/)).toBeInTheDocument()
    expect(screen.getByText(/b failed/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry Task' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start over' })).toBeInTheDocument()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Continue execution' }))
    await user.type(screen.getByRole('textbox', { name: 'Additional instructions (optional)' }), 'Configuration fixed')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Continue execution' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith(identity, value.id, value.version, 10, 'Configuration fixed'))
    await waitFor(() => expect(screen.queryByText('Some tasks need attention')).not.toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
  })

  it('retains recovery instructions when a conflict refreshes the WorkItem version', async () => {
    const source = failedWorkflow()
    vi.spyOn(api, 'resumeWorkflow').mockImplementation(async () => {
      vi.mocked(api.getWorkItem).mockResolvedValue(context({ ...source, version: 2 }))
      throw new Error('Version changed')
    })
    renderPage()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Continue execution' }))
    await user.type(screen.getByRole('textbox', { name: 'Additional instructions (optional)' }), 'Keep these notes')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Continue execution' }))
    expect(await screen.findByText('Version changed')).toBeInTheDocument()
    await waitFor(() => expect(api.getWorkItem).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('textbox', { name: 'Additional instructions (optional)' })).toHaveValue('Keep these notes')
  })

  it('shows task-only failures without management controls to Agents', async () => {
    const source = failedWorkflow()
    vi.mocked(api.getWorkItem).mockResolvedValue(context({ ...source, status: 'open', failure: null }, [failedTask('a')]))
    renderPage({ id: 'developer', kind: 'agent', role: 'developer' })
    expect(await screen.findByText('Some tasks need attention')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Start over' })).not.toBeInTheDocument()
  })
})


describe('Interrupted Workflow recovery capacity', () => {
  it.each([2, 498, 499])('counts both failed attempts of a node with %i instances', async count => {
    const source = failedWorkflow('execution_failure', count)
    const tasks: Task[] = Array.from({ length: count }, (_, index) => ({
      ...completedTask(), id: `a-${index}`, workflow_task_id: 'a', position: index,
      status: index >= count - 2 ? 'failed' : 'completed',
    }))
    // Other nodes' history must not consume A's execution budget.
    tasks.push({ ...completedTask(), id: 'b-0', workflow_task_id: 'b', position: count })
    vi.mocked(api.getWorkItem).mockResolvedValue(context(source, tasks))
    renderPage()
    if (count === 499) {
      expect(await screen.findByText(/supported maximum of 500/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
    } else {
      await userEvent.setup().click(await screen.findByRole('button', { name: 'Continue execution' }))
      expect(screen.getByRole('spinbutton')).toHaveValue(count + 2)
      expect(screen.getByRole('spinbutton')).toHaveAttribute('min', String(count + 2))
    }
  })

  it.each([2, 499, 500])('includes interrupted attempts when the node has %i instances', async count => {
    const source = failedWorkflow('execution_failure', count)
    const tasks: Task[] = Array.from({ length: count }, (_, index) => ({
      ...completedTask(), id: `a-${index}`, workflow_task_id: 'a', position: index,
      retry_of_task_id: index ? `a-${index - 1}` : null,
      status: index === count - 1 ? 'pending' : 'failed', completed_at: null,
    }))
    const value = context(source, tasks)
    value.recovery_task_ids = [tasks[count - 1].id]
    vi.mocked(api.getWorkItem).mockResolvedValue(value)
    renderPage()
    const user = userEvent.setup()
    if (count === 500) {
      expect(await screen.findByText(/supported maximum of 500/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Continue execution' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Start over' })).toBeInTheDocument()
    } else {
      await user.click(await screen.findByRole('button', { name: 'Continue execution' }))
      expect(screen.getByRole('spinbutton')).toHaveValue(count + 1)
    }
  })
})
