import { useEffect, useMemo, useRef, useState } from 'react'
import { Background, Controls, MarkerType, Position, ReactFlow, applyNodeChanges, type Connection, type Edge, type Node, type NodeChange, type ReactFlowInstance, type XYPosition } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useI18n } from './i18n'
import type { WorkflowDraft } from './workflowDraft'
import { workflowEdgePresentation, workflowEdgeTypes, workflowNodeTypes } from './WorkflowGraphEdges'

const nodeWidth = 190
const nodeHeight = 74
const rowGap = 110
const stagingRows = 4

function workflowColumnGap(relations: WorkflowDraft['relations']) {
  return relations.some(relation => relation.label?.trim()) ? 330 : 250
}

export function workflowEditorLayout(tasks: WorkflowDraft['tasks'], relations: WorkflowDraft['relations'], starts: string[]) {
  const columnGap = workflowColumnGap(relations)
  const depth = new Map<string, number>()
  const queue = starts.map(id => ({ id, depth: 0 }))
  while (queue.length > 0) {
    const current = queue.shift()!
    if (depth.has(current.id)) continue
    depth.set(current.id, current.depth)
    relations.filter(relation => relation.from_task_id === current.id).forEach(relation => queue.push({ id: relation.to_task_id, depth: current.depth + 1 }))
  }
  const stagingColumn = depth.size > 0 ? Math.max(...depth.values()) + 1 : 0
  let stagingIndex = 0
  const rows = new Map<number, number>()
  return new Map(tasks.map(task => {
    const graphDepth = depth.get(task.id)
    if (graphDepth === undefined) {
      const column = stagingColumn + Math.floor(stagingIndex / stagingRows)
      const row = stagingIndex % stagingRows
      stagingIndex++
      return [task.id, { x: column * columnGap, y: row * rowGap }]
    }
    const row = rows.get(graphDepth) ?? 0
    rows.set(graphDepth, row + 1)
    return [task.id, { x: graphDepth * columnGap, y: row * rowGap }]
  }))
}

export function workflowTaskPositionNearAnchor(anchor: XYPosition, occupied: XYPosition[], columnGap: number): XYPosition {
  const position = { x: anchor.x + columnGap, y: anchor.y }
  while (occupied.some(other => Math.abs(other.x - position.x) < nodeWidth && Math.abs(other.y - position.y) < nodeHeight)) {
    position.y += rowGap
  }
  return position
}

export interface WorkflowTaskPlacement { taskID: string; anchorTaskID: string | null }

export function WorkflowEditorMap({ draft, selectedTaskID, selectedRelationID, newTaskPlacement, onTaskPlaced, onSelectTask, onSelectRelation, onConnect, onDeleteRelation }: {
  draft: WorkflowDraft; selectedTaskID: string | null; selectedRelationID: string | null
  newTaskPlacement: WorkflowTaskPlacement | null; onTaskPlaced: (taskID: string) => void
  onSelectTask: (id: string) => void; onSelectRelation: (id: string | null) => void
  onConnect: (from: string, to: string) => void; onDeleteRelation: (id: string) => void
}) {
  const { t } = useI18n()
  const [nodes, setNodes] = useState<Node[]>([])
  const [flow, setFlow] = useState<ReactFlowInstance<Node, Edge> | null>(null)
  const manuallyPositioned = useRef(new Set<string>())
  const topologyKey = useMemo(() => JSON.stringify({
    tasks: draft.tasks.map(task => task.id),
    starts: draft.startTaskIDs,
    relations: draft.relations.map(relation => [relation.id, relation.from_task_id, relation.to_task_id, Boolean(relation.label?.trim())]),
  }), [draft.tasks, draft.startTaskIDs, draft.relations])
  const previousTopologyKey = useRef(topologyKey)
  useEffect(() => {
    setNodes(current => {
      const existing = new Map(current.map(node => [node.id, node.position]))
      const automatic = workflowEditorLayout(draft.tasks, draft.relations, draft.startTaskIDs)
      const topologyChanged = previousTopologyKey.current !== topologyKey
      const occupied = [...existing.values()]
      return draft.tasks.map(task => ({
        id: task.id,
        position: newTaskPlacement?.taskID === task.id && !existing.has(task.id) && newTaskPlacement.anchorTaskID && existing.has(newTaskPlacement.anchorTaskID)
          ? workflowTaskPositionNearAnchor(existing.get(newTaskPlacement.anchorTaskID)!, occupied, workflowColumnGap(draft.relations))
          : existing.has(task.id) && (!topologyChanged || manuallyPositioned.current.has(task.id))
          ? existing.get(task.id)!
          : automatic.get(task.id) ?? { x: 0, y: 0 },
        data: { label: <div className="definition-node-content"><span>{draft.startTaskIDs.includes(task.id) ? t('startNode') : t(task.executor)}</span><strong>{task.title}</strong><small>{t(task.execution === 'optional' ? 'optionalTask' : 'requiredTask')}</small></div> },
        type: 'workflowTask', className: `definition-flow-node editable ${selectedTaskID === task.id ? 'selected' : ''}`,
        sourcePosition: Position.Right, targetPosition: Position.Left,
      }))
    })
    previousTopologyKey.current = topologyKey
    manuallyPositioned.current = new Set([...manuallyPositioned.current].filter(taskID => draft.tasks.some(task => task.id === taskID)))
  }, [draft.tasks, draft.startTaskIDs, draft.relations, newTaskPlacement, selectedTaskID, t, topologyKey])
  useEffect(() => {
    if (!flow || !newTaskPlacement) return
    const node = nodes.find(item => item.id === newTaskPlacement.taskID)
    if (!node) return
    const frame = window.requestAnimationFrame(() => {
      const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
      const zoom = Math.min(Math.max(flow.getZoom(), .7), 1.1)
      void flow.setCenter(node.position.x + nodeWidth / 2, node.position.y + nodeHeight / 2, { zoom, duration: reducedMotion ? 0 : 220 })
      onTaskPlaced(node.id)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [flow, newTaskPlacement, nodes, onTaskPlaced])
  const edges: Edge[] = draft.relations.map(relation => ({
    id: relation.id, source: relation.from_task_id, target: relation.to_task_id, ...workflowEdgePresentation(relation.from_task_id, relation.to_task_id, (nodes.find(node => node.id === relation.from_task_id)?.position.x ?? 0) > (nodes.find(node => node.id === relation.to_task_id)?.position.x ?? 0)),
    markerEnd: { type: MarkerType.ArrowClosed }, className: selectedRelationID === relation.id ? 'selected-relation' : '',
    label: relation.label || undefined,
    labelClassName: 'workflow-edge-label', labelBgPadding: [7, 4], labelBgBorderRadius: 5,
  }))
  const handleNodesChange = (changes: NodeChange[]) => setNodes(current => applyNodeChanges(changes, current))
  const connect = (connection: Connection) => { if (connection.source && connection.target) onConnect(connection.source, connection.target) }
  return <div className="workflow-editor-graph"><ReactFlow nodes={nodes} edges={edges} edgeTypes={workflowEdgeTypes} nodeTypes={workflowNodeTypes} onInit={setFlow} onNodesChange={handleNodesChange} onNodeDragStop={(_, node) => manuallyPositioned.current.add(node.id)} onConnect={connect} onNodeClick={(_, node) => onSelectTask(node.id)} onPaneClick={() => { onSelectTask(''); onSelectRelation(null) }} onEdgeClick={(_, edge) => onSelectRelation(edge.id)} onEdgesDelete={deleted => deleted.forEach(edge => onDeleteRelation(edge.id))} deleteKeyCode={['Backspace', 'Delete']} fitView fitViewOptions={{ padding: .2 }} minZoom={.15} maxZoom={1.6} nodesDraggable nodesConnectable elementsSelectable proOptions={{ hideAttribution: true }}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}
