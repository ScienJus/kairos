import { BaseEdge, Handle, Position, type EdgeProps, type EdgeTypes, type NodeProps, type NodeTypes } from '@xyflow/react'
import type { ReactNode } from 'react'

function WorkflowSelfLoopEdge({ sourceX, sourceY, targetX, targetY, markerEnd, style }: EdgeProps) {
  const lift = 38
  const spread = 22
  const path = `M ${sourceX} ${sourceY} C ${sourceX + spread} ${sourceY - lift}, ${targetX - spread} ${targetY - lift}, ${targetX} ${targetY}`
  return <BaseEdge path={path} markerEnd={markerEnd} style={style} />
}

function WorkflowCycleBackEdge({ sourceX, sourceY, targetX, targetY, markerEnd, style }: EdgeProps) {
  const drop = 44
  const path = `M ${sourceX} ${sourceY} C ${sourceX} ${sourceY + drop}, ${targetX} ${targetY + drop}, ${targetX} ${targetY}`
  return <BaseEdge path={path} markerEnd={markerEnd} style={style} />
}

function WorkflowGraphNode({ data }: NodeProps) {
  return <>
    {(data as { label: ReactNode }).label}
    <Handle id="incoming" type="target" position={Position.Left} />
    <Handle id="outgoing" type="source" position={Position.Right} />
    <Handle id="loop-in" className="workflow-loop-handle" type="target" position={Position.Top} style={{ left: '36%' }} />
    <Handle id="loop-out" className="workflow-loop-handle" type="source" position={Position.Top} style={{ left: '64%' }} />
    <Handle id="cycle-out" className="workflow-cycle-handle" type="source" position={Position.Bottom} style={{ left: '36%' }} />
    <Handle id="cycle-in" className="workflow-cycle-handle" type="target" position={Position.Bottom} style={{ left: '64%' }} />
  </>
}

export const workflowEdgeTypes: EdgeTypes = { selfLoop: WorkflowSelfLoopEdge, cycleBack: WorkflowCycleBackEdge }
export const workflowNodeTypes: NodeTypes = { workflowTask: WorkflowGraphNode }

export function workflowNodeIsSelected(runtimeTaskIDs: string[], selectedTaskID: string | null) {
  return selectedTaskID !== null && runtimeTaskIDs.includes(selectedTaskID)
}

export function workflowGraphLayout(
  taskIDs: string[],
  relations: Array<{ FromTaskID: string; ToTaskID: string }>,
  startTaskIDs: string[],
) {
  const knownTaskIDs = new Set(taskIDs)
  const validRelations = relations.filter(relation => knownTaskIDs.has(relation.FromTaskID) && knownTaskIDs.has(relation.ToTaskID))
  const outgoing = new Map(taskIDs.map(taskID => [taskID, [] as typeof validRelations]))
  validRelations.forEach(relation => outgoing.get(relation.FromTaskID)!.push(relation))

  const edgeKey = (fromTaskID: string, toTaskID: string) => JSON.stringify([fromTaskID, toTaskID])
  const backEdges = new Set<string>()
  const state = new Map<string, 'visiting' | 'visited'>()
  // Removing DFS back edges leaves a DAG whose longest paths provide stable columns.
  const visit = (taskID: string) => {
    state.set(taskID, 'visiting')
    outgoing.get(taskID)!.forEach(relation => {
      const targetState = state.get(relation.ToTaskID)
      if (targetState === 'visiting') backEdges.add(edgeKey(relation.FromTaskID, relation.ToTaskID))
      else if (!targetState) visit(relation.ToTaskID)
    })
    state.set(taskID, 'visited')
  }
  startTaskIDs.filter(taskID => knownTaskIDs.has(taskID)).forEach(taskID => { if (!state.has(taskID)) visit(taskID) })
  taskIDs.forEach(taskID => { if (!state.has(taskID)) visit(taskID) })

  const forwardRelations = validRelations.filter(relation => !backEdges.has(edgeKey(relation.FromTaskID, relation.ToTaskID)))
  const forwardOutgoing = new Map(taskIDs.map(taskID => [taskID, [] as typeof forwardRelations]))
  const incoming = new Map(taskIDs.map(taskID => [taskID, 0]))
  forwardRelations.forEach(relation => {
    forwardOutgoing.get(relation.FromTaskID)!.push(relation)
    incoming.set(relation.ToTaskID, incoming.get(relation.ToTaskID)! + 1)
  })
  const depths = new Map(taskIDs.map(taskID => [taskID, 0]))
  const queue = taskIDs.filter(taskID => incoming.get(taskID) === 0)
  while (queue.length > 0) {
    const current = queue.shift()!
    forwardOutgoing.get(current)!.forEach(relation => {
      depths.set(relation.ToTaskID, Math.max(depths.get(relation.ToTaskID)!, depths.get(current)! + 1))
      incoming.set(relation.ToTaskID, incoming.get(relation.ToTaskID)! - 1)
      if (incoming.get(relation.ToTaskID) === 0) queue.push(relation.ToTaskID)
    })
  }
  return {
    depths,
    isBackEdge: (fromTaskID: string, toTaskID: string) => backEdges.has(edgeKey(fromTaskID, toTaskID)),
  }
}

export function workflowEdgePresentation(fromTaskID: string, toTaskID: string, goesBackward = false) {
  if (fromTaskID === toTaskID) return { type: 'selfLoop', sourceHandle: 'loop-out', targetHandle: 'loop-in' }
  if (goesBackward) return { type: 'cycleBack', sourceHandle: 'cycle-out', targetHandle: 'cycle-in' }
  return { type: 'smoothstep', sourceHandle: 'outgoing', targetHandle: 'incoming' }
}
