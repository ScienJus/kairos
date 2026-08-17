import { describe, expect, it } from 'vitest'
import { workflowEdgePresentation } from './WorkflowGraphEdges'

describe('Workflow graph edge rendering', () => {
  it('uses the shared self-loop renderer in editor and definition maps', () => {
    expect(workflowEdgePresentation('verify', 'verify')).toEqual({ type: 'selfLoop', sourceHandle: 'loop-out', targetHandle: 'loop-in' })
    expect(workflowEdgePresentation('verify', 'publish')).toEqual({ type: 'smoothstep', sourceHandle: 'outgoing', targetHandle: 'incoming' })
    expect(workflowEdgePresentation('fix', 'verify', true)).toEqual({ type: 'cycleBack', sourceHandle: 'cycle-out', targetHandle: 'cycle-in' })
  })
})
