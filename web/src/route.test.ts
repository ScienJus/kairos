import { describe, expect, it } from 'vitest'
import { readRoute, routePath } from './route'

describe('route state', () => {
  it('maps homepage tabs to stable URLs', () => {
    expect(routePath({ workItemID: null, taskID: null, homeView: 'all' })).toBe('/')
    expect(routePath({ workItemID: null, taskID: null, homeView: 'human' })).toBe('/attention')
    expect(readRoute('/attention').homeView).toBe('human')
  })

  it('round trips encoded WorkItem and Task IDs', () => {
    const route = { workItemID: 'work item', taskID: 'task/item', homeView: 'all' as const }
    expect(readRoute(routePath(route))).toEqual(route)
  })

  it('falls back to the homepage for malformed URL encoding', () => {
    const homepage = { workItemID: null, taskID: null, homeView: 'all' }
    expect(readRoute('/work-items/%ZZ')).toEqual(homepage)
    expect(readRoute('/work-items/%E0%A4')).toEqual(homepage)
  })

  it('round trips Blackboard definition versions', () => {
    const route = { workItemID: null, taskID: null, homeView: 'all' as const, blackboardID: 'product work', blackboardVersion: 3 }
    expect(routePath(route)).toBe('/blackboards/product%20work/versions/3')
    expect(readRoute(routePath(route))).toEqual(route)
    expect(routePath({ workItemID: null, taskID: null, homeView: 'all', blackboardID: null })).toBe('/blackboards')
  })

  it('round trips Workflow definition versions', () => {
    const route = { workItemID: null, taskID: null, homeView: 'all' as const, workflowID: 'release flow', workflowVersion: 2 }
    expect(routePath(route)).toBe('/workflows/release%20flow/versions/2')
    expect(readRoute(routePath(route))).toEqual(route)
    expect(routePath({ workItemID: null, taskID: null, homeView: 'all', workflowID: null })).toBe('/workflows')
  })

  it('keeps Workflow editor routes stable', () => {
    const edit = { workItemID: null, taskID: null, homeView: 'all' as const, workflowID: 'release', workflowVersion: 2, workflowEditing: true }
    expect(routePath(edit)).toBe('/workflows/release/versions/2/edit')
    expect(readRoute(routePath(edit))).toEqual(edit)
    expect(readRoute('/workflows/new')).toEqual({ workItemID: null, taskID: null, homeView: 'all', workflowID: null, workflowVersion: null, workflowEditing: true })
  })
})
