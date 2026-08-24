import { BaseEdge, EdgeLabelRenderer, Handle, Position, type EdgeProps, type EdgeTypes, type NodeProps, type NodeTypes } from '@xyflow/react'
import type { ReactNode } from 'react'

function CurvedEdgeLabel({ label, x, y }: { label: ReactNode; x: number; y: number }) {
  if (!label) return null
  return <EdgeLabelRenderer><div className="workflow-edge-label custom" style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }}>{label}</div></EdgeLabelRenderer>
}

function WorkflowSelfLoopEdge({ sourceX, sourceY, targetX, targetY, markerEnd, style, label }: EdgeProps) {
  const lift = 38
  const spread = 22
  const path = `M ${sourceX} ${sourceY} C ${sourceX + spread} ${sourceY - lift}, ${targetX - spread} ${targetY - lift}, ${targetX} ${targetY}`
  return <><BaseEdge path={path} markerEnd={markerEnd} style={style} /><CurvedEdgeLabel label={label} x={(sourceX + targetX) / 2} y={Math.min(sourceY, targetY) - lift * .7} /></>
}

function WorkflowCycleBackEdge({ sourceX, sourceY, targetX, targetY, markerEnd, style, label }: EdgeProps) {
  const drop = 44
  const path = `M ${sourceX} ${sourceY} C ${sourceX} ${sourceY + drop}, ${targetX} ${targetY + drop}, ${targetX} ${targetY}`
  return <><BaseEdge path={path} markerEnd={markerEnd} style={style} /><CurvedEdgeLabel label={label} x={(sourceX + targetX) / 2} y={Math.max(sourceY, targetY) + drop * .72} /></>
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
  relations: Array<{ from_task_id: string; to_task_id: string }>,
  startTaskIDs: string[],
) {
  const knownTaskIDs = new Set(taskIDs)
  const validRelations = relations.filter(relation => knownTaskIDs.has(relation.from_task_id) && knownTaskIDs.has(relation.to_task_id))
  const outgoing = new Map(taskIDs.map(taskID => [taskID, [] as typeof validRelations]))
  validRelations.forEach(relation => outgoing.get(relation.from_task_id)!.push(relation))

  const edgeKey = (fromTaskID: string, toTaskID: string) => JSON.stringify([fromTaskID, toTaskID])
  const backEdges = new Set<string>()
  const state = new Map<string, 'visiting' | 'visited'>()
  // Removing DFS back edges leaves a DAG whose longest paths provide stable columns.
  const visit = (taskID: string) => {
    state.set(taskID, 'visiting')
    outgoing.get(taskID)!.forEach(relation => {
      const targetState = state.get(relation.to_task_id)
      if (targetState === 'visiting') backEdges.add(edgeKey(relation.from_task_id, relation.to_task_id))
      else if (!targetState) visit(relation.to_task_id)
    })
    state.set(taskID, 'visited')
  }
  startTaskIDs.filter(taskID => knownTaskIDs.has(taskID)).forEach(taskID => { if (!state.has(taskID)) visit(taskID) })
  taskIDs.forEach(taskID => { if (!state.has(taskID)) visit(taskID) })

  const forwardRelations = validRelations.filter(relation => !backEdges.has(edgeKey(relation.from_task_id, relation.to_task_id)))
  const forwardOutgoing = new Map(taskIDs.map(taskID => [taskID, [] as typeof forwardRelations]))
  const incoming = new Map(taskIDs.map(taskID => [taskID, 0]))
  forwardRelations.forEach(relation => {
    forwardOutgoing.get(relation.from_task_id)!.push(relation)
    incoming.set(relation.to_task_id, incoming.get(relation.to_task_id)! + 1)
  })
  const depths = new Map(taskIDs.map(taskID => [taskID, 0]))
  const queue = taskIDs.filter(taskID => incoming.get(taskID) === 0)
  while (queue.length > 0) {
    const current = queue.shift()!
    forwardOutgoing.get(current)!.forEach(relation => {
      depths.set(relation.to_task_id, Math.max(depths.get(relation.to_task_id)!, depths.get(current)! + 1))
      incoming.set(relation.to_task_id, incoming.get(relation.to_task_id)! - 1)
      if (incoming.get(relation.to_task_id) === 0) queue.push(relation.to_task_id)
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
