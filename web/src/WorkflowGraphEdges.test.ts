import { describe, expect, it } from 'vitest'
import { workflowEdgePresentation, workflowGraphLayout, workflowNodeIsSelected } from './WorkflowGraphEdges'

describe('Workflow graph edge rendering', () => {
  it('uses the shared self-loop renderer in editor and definition maps', () => {
    expect(workflowEdgePresentation('verify', 'verify')).toEqual({ type: 'selfLoop', sourceHandle: 'loop-out', targetHandle: 'loop-in' })
    expect(workflowEdgePresentation('verify', 'publish')).toEqual({ type: 'smoothstep', sourceHandle: 'outgoing', targetHandle: 'incoming' })
    expect(workflowEdgePresentation('fix', 'verify', true)).toEqual({ type: 'cycleBack', sourceHandle: 'cycle-out', targetHandle: 'cycle-in' })
  })

  it('keeps cyclic workflow nodes in forward execution order so the back edge remains visible', () => {
    const layout = workflowGraphLayout(['write', 'check', 'delivery'], [
      { FromTaskID: 'write', ToTaskID: 'check' },
      { FromTaskID: 'check', ToTaskID: 'write' },
      { FromTaskID: 'check', ToTaskID: 'delivery' },
    ], ['write'])

    expect([...layout.depths]).toEqual([['write', 0], ['check', 1], ['delivery', 2]])
    expect(layout.isBackEdge('write', 'check')).toBe(false)
    expect(layout.isBackEdge('check', 'write')).toBe(true)
  })

  it('places a join after its longest forward path without treating a transitive edge as a cycle', () => {
    const layout = workflowGraphLayout(['analysis', 'development', 'release'], [
      { FromTaskID: 'analysis', ToTaskID: 'development' },
      { FromTaskID: 'analysis', ToTaskID: 'release' },
      { FromTaskID: 'development', ToTaskID: 'release' },
    ], ['analysis'])

    expect([...layout.depths]).toEqual([['analysis', 0], ['development', 1], ['release', 2]])
    expect(layout.isBackEdge('development', 'release')).toBe(false)
  })

  it('keeps a workflow node selected while viewing any of its runtime instances', () => {
    const runtimeTaskIDs = ['write-1', 'write-2', 'write-3']

    expect(workflowNodeIsSelected(runtimeTaskIDs, 'write-1')).toBe(true)
    expect(workflowNodeIsSelected(runtimeTaskIDs, 'write-3')).toBe(true)
    expect(workflowNodeIsSelected(runtimeTaskIDs, 'check-1')).toBe(false)
    expect(workflowNodeIsSelected(runtimeTaskIDs, null)).toBe(false)
  })
})
