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

export function workflowEdgePresentation(fromTaskID: string, toTaskID: string, goesBackward = false) {
  if (fromTaskID === toTaskID) return { type: 'selfLoop', sourceHandle: 'loop-out', targetHandle: 'loop-in' }
  if (goesBackward) return { type: 'cycleBack', sourceHandle: 'cycle-out', targetHandle: 'cycle-in' }
  return { type: 'smoothstep', sourceHandle: 'outgoing', targetHandle: 'incoming' }
}
