// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { APIError, api } from './api'
import { DaemonsPage } from './DaemonsPage'
import { I18nProvider } from './i18n'
import type { DaemonInstance, Identity } from './types'

const identity: Identity = { id: 'operator', kind: 'human', role: '' }
const instance: DaemonInstance = {
  id: '4d383a89-154a-4794-bac3-e61cb8b3a6e4', name: 'Office', agent_id: 'agent-a',
  process_started_at: '2026-09-20T12:00:00Z', registered_at: '2026-09-20T12:00:00Z',
  last_report_at: '2026-09-20T12:01:00Z', stopped_at: null, daemon_version: 'dev', adapter: 'codex',
  slots: 2, tags: [], lifecycle: 'running', health: 'paused', connectivity: 'stale', as_of: '2026-09-20T12:02:00Z',
  active_count: 1, active_dispatches: [{ id: 'dispatch-a', candidate_kind: 'task', work_item_id: 'work-a',
    task_id: 'task-a', claim_id: 'claim-a', claim_status: 'uncertain', state: 'running', attempts: 1, started_at: '2026-09-20T12:00:30Z' }],
  omitted_active_count: 0, dropped_events: 0, last_revision: 2,
}

beforeEach(() => {
  localStorage.setItem('kairos-console-locale', 'en')
  vi.spyOn(api, 'listDaemons').mockResolvedValue({ data: [instance], next_cursor: null, as_of: instance.as_of })
  vi.spyOn(api, 'getDaemon').mockResolvedValue(instance)
  vi.spyOn(api, 'listDaemonEvents').mockResolvedValue({ data: [{ sequence: 1, kind: 'dispatch_ended', candidate_kind: 'task', occurred_at: '2026-09-20T12:00:30Z', received_at: '2026-09-20T12:00:31Z', dispatch_id: 'dispatch-a', work_item_id: 'work-a', task_id: 'task-a', claim_id: 'claim-a', details: { state: 'finished', outcome: 'candidate_declined', applied: false, attempts: 2, duration_ms: 2000 } }], next_cursor: null })
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear() })

function renderPage(daemonID: string | null, navigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><I18nProvider><DaemonsPage identity={identity} daemonID={daemonID} navigate={navigate} /></I18nProvider></QueryClientProvider>)
  return navigate
}

it('shows an instance card before opening its work and history', async () => {
  const navigate = renderPage(null)
  expect(await screen.findByRole('heading', { name: 'Local agents' })).toBeInTheDocument()
  expect(await screen.findByText('One dispatch was in progress in the last report.')).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Work in the last report' })).not.toBeInTheDocument()
  expect(api.getDaemon).not.toHaveBeenCalled()
  await userEvent.setup().click(screen.getByRole('button', { name: /Office/ }))
  expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ daemonID: instance.id }))
})

it('keeps separate instances recognizable when they share a name', async () => {
  const second = { ...instance, id: 'b357a99e-1b52-419e-a8d4-184211cb7a6f' }
  vi.mocked(api.listDaemons).mockResolvedValue({ data: [instance, second], next_cursor: null, as_of: instance.as_of })
  const navigate = renderPage(null)
  expect(await screen.findByText('#4d383a')).toBeInTheDocument()
  expect(screen.getByText('#b357a9')).toBeInTheDocument()
  await userEvent.setup().click(screen.getByRole('button', { name: /#b357a9/ }))
  expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ daemonID: second.id }))
})

it('shows stale work as a last report and keeps metadata behind technical details', async () => {
  const navigate = vi.fn()
  renderPage(instance.id, navigate)
  expect(await screen.findByRole('heading', { name: 'Office' })).toBeInTheDocument()
  expect(screen.getByText('No recent report')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Work in the last report' })).toBeInTheDocument()
  expect(screen.getByText(/Claim response uncertain/)).toBeInTheDocument()
  expect(await screen.findByText('Finished a dispatch')).toBeInTheDocument()
  expect(screen.getByText(/Task work.*Attempt 2.*Declined this work.*Outcome not confirmed.*2000 ms/)).toBeInTheDocument()
  expect(screen.getByText(instance.id)).not.toBeVisible()
  const user = userEvent.setup()
  await user.click(screen.getByText('Technical details'))
  expect(screen.getByText(instance.id)).toBeVisible()
  expect(screen.getByText('Paused admission')).toBeVisible()
  await user.click(screen.getAllByRole('button', { name: /Open related work/ })[0])
  expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ workItemID: 'work-a', taskID: 'task-a' }))
})

it('states the event retention window when no retained activity is available', async () => {
  vi.mocked(api.listDaemonEvents).mockResolvedValue({ data: [], next_cursor: null })
  renderPage(instance.id)
  expect(await screen.findByText('No activity is available from the last 30 days.')).toBeInTheDocument()
  expect(screen.getByText('Activity records are retained for 30 days.')).toBeInTheDocument()
})

it('shows a fresh stopping instance as shutting down', async () => {
  vi.mocked(api.listDaemons).mockResolvedValue({ data: [{ ...instance, connectivity: 'reporting', lifecycle: 'stopping', health: 'healthy' }], next_cursor: null, as_of: instance.as_of })
  renderPage(null)
  expect(await screen.findByText('Stopping')).toBeInTheDocument()
  expect(screen.getByText('Shutting down; no new work will be accepted.')).toBeInTheDocument()
})

it('does not describe an initial list failure as an empty result', async () => {
  vi.mocked(api.listDaemons).mockRejectedValue(new APIError(500, 'unavailable'))
  renderPage(null)
  expect(await screen.findByText('Kairos could not refresh these observations. The last received report may still be shown.')).toBeInTheDocument()
  expect(screen.queryByText('No Daemons have reported recently.')).not.toBeInTheDocument()
})

it('does not declare the present section empty while more pages remain', async () => {
  vi.mocked(api.listDaemons).mockResolvedValue({
    data: [{ ...instance, lifecycle: 'stopped', connectivity: 'stopped', stopped_at: instance.last_report_at }],
    next_cursor: 'next-page', as_of: instance.as_of,
  })
  renderPage(null)
  expect(await screen.findByRole('button', { name: /Office/ })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Load more' })).toBeInTheDocument()
  expect(screen.queryByText('No Daemons have reported recently.')).not.toBeInTheDocument()
})

it.each([
  [new APIError(404, 'not found'), 'This Daemon could not be found.'],
  [new APIError(500, 'unavailable'), 'Kairos could not refresh these observations. The last received report may still be shown.'],
])('classifies an initial detail error by status', async (error, message) => {
  vi.mocked(api.getDaemon).mockRejectedValue(error)
  renderPage(instance.id)
  expect(await screen.findByText(message)).toBeInTheDocument()
})
