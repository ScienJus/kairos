import { describe, expect, it } from 'vitest'
import type { WorkflowRelationDefinition, WorkflowTaskDefinition } from './types'
import { workflowEditorLayout, workflowTaskPositionNearAnchor } from './WorkflowEditorMap'

const task = (ID: string): WorkflowTaskDefinition => ({
  ID, Title: ID, Description: '', AcceptanceCriteria: '', Executor: 'agent',
  AllowedRoles: [], Execution: 'required', ReviewPolicy: 'none', DefaultTags: [], Artifacts: [],
})

describe('Workflow editor layout', () => {
  it('packs unconnected tasks into staging rows instead of one column per task', () => {
    const positions = workflowEditorLayout(
      ['start', 'a', 'b', 'c', 'd', 'e'].map(task),
      [],
      ['start'],
    )

    expect(positions.get('a')).toEqual({ x: 250, y: 0 })
    expect(positions.get('b')).toEqual({ x: 250, y: 110 })
    expect(positions.get('d')).toEqual({ x: 250, y: 330 })
    expect(positions.get('e')).toEqual({ x: 500, y: 0 })
  })

  it('reserves wider graph columns only when a relation has a visible label', () => {
    const tasks = [task('start'), task('verify')]
    const plain: WorkflowRelationDefinition[] = [{ ID: 'plain', FromTaskID: 'start', ToTaskID: 'verify' }]
    const labeled: WorkflowRelationDefinition[] = [{ ...plain[0], Label: 'Needs verification' }]

    expect(workflowEditorLayout(tasks, plain, ['start']).get('verify')?.x).toBe(250)
    expect(workflowEditorLayout(tasks, labeled, ['start']).get('verify')?.x).toBe(330)
  })

  it('places a new task beside its anchor and avoids occupied rows', () => {
    expect(workflowTaskPositionNearAnchor(
      { x: 0, y: 0 },
      [{ x: 250, y: 0 }, { x: 250, y: 110 }],
      250,
    )).toEqual({ x: 250, y: 220 })
  })
})
