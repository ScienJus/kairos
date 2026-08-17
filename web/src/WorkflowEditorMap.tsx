import { useEffect, useMemo, useState } from 'react'
import { Background, Controls, MarkerType, Position, ReactFlow, applyNodeChanges, type Connection, type Edge, type Node, type NodeChange } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useI18n } from './i18n'
import type { WorkflowDraft } from './workflowDraft'
import { workflowEdgePresentation, workflowEdgeTypes, workflowNodeTypes } from './WorkflowGraphEdges'

function layout(tasks: WorkflowDraft['tasks'], relations: WorkflowDraft['relations'], starts: string[]) {
  const depth = new Map<string, number>()
  const queue = starts.map(id => ({ id, depth: 0 }))
  while (queue.length > 0) {
    const current = queue.shift()!
    if (depth.has(current.id)) continue
    depth.set(current.id, current.depth)
    relations.filter(relation => relation.FromTaskID === current.id).forEach(relation => queue.push({ id: relation.ToTaskID, depth: current.depth + 1 }))
  }
  let fallback = Math.max(0, ...depth.values()) + 1
  tasks.forEach(task => { if (!depth.has(task.ID)) depth.set(task.ID, fallback++) })
  const rows = new Map<number, number>()
  return new Map(tasks.map(task => {
    const column = depth.get(task.ID) ?? 0
    const row = rows.get(column) ?? 0
    rows.set(column, row + 1)
    return [task.ID, { x: column * 250, y: row * 110 }]
  }))
}

export function WorkflowEditorMap({ draft, selectedTaskID, selectedRelationID, onSelectTask, onSelectRelation, onConnect, onDeleteRelation }: {
  draft: WorkflowDraft; selectedTaskID: string | null; selectedRelationID: string | null
  onSelectTask: (id: string) => void; onSelectRelation: (id: string | null) => void
  onConnect: (from: string, to: string) => void; onDeleteRelation: (id: string) => void
}) {
  const { t } = useI18n()
  const initialPositions = useMemo(() => layout(draft.tasks, draft.relations, draft.startTaskIDs), [])
  const [nodes, setNodes] = useState<Node[]>([])
  useEffect(() => {
    setNodes(current => {
      const existing = new Map(current.map(node => [node.id, node.position]))
      const fallback = layout(draft.tasks, draft.relations, draft.startTaskIDs)
      return draft.tasks.map(task => ({
        id: task.ID, position: existing.get(task.ID) ?? initialPositions.get(task.ID) ?? fallback.get(task.ID) ?? { x: 0, y: 0 },
        data: { label: <div className="definition-node-content"><span>{draft.startTaskIDs.includes(task.ID) ? t('startNode') : t(task.Executor)}</span><strong>{task.Title}</strong><small>{t(task.Execution === 'optional' ? 'optionalTask' : 'requiredTask')}</small></div> },
        type: 'workflowTask', className: `definition-flow-node editable ${selectedTaskID === task.ID ? 'selected' : ''}`,
        sourcePosition: Position.Right, targetPosition: Position.Left,
      }))
    })
  }, [draft.tasks, draft.startTaskIDs, selectedTaskID, t])
  const edges: Edge[] = draft.relations.map(relation => ({
    id: relation.ID, source: relation.FromTaskID, target: relation.ToTaskID, ...workflowEdgePresentation(relation.FromTaskID, relation.ToTaskID, (nodes.find(node => node.id === relation.FromTaskID)?.position.x ?? 0) > (nodes.find(node => node.id === relation.ToTaskID)?.position.x ?? 0)),
    markerEnd: { type: MarkerType.ArrowClosed }, className: selectedRelationID === relation.ID ? 'selected-relation' : '',
  }))
  const handleNodesChange = (changes: NodeChange[]) => setNodes(current => applyNodeChanges(changes, current))
  const connect = (connection: Connection) => { if (connection.source && connection.target) onConnect(connection.source, connection.target) }
  return <div className="workflow-editor-graph"><ReactFlow nodes={nodes} edges={edges} edgeTypes={workflowEdgeTypes} nodeTypes={workflowNodeTypes} onNodesChange={handleNodesChange} onConnect={connect} onNodeClick={(_, node) => onSelectTask(node.id)} onPaneClick={() => { onSelectTask(''); onSelectRelation(null) }} onEdgeClick={(_, edge) => onSelectRelation(edge.id)} onEdgesDelete={deleted => deleted.forEach(edge => onDeleteRelation(edge.id))} deleteKeyCode={['Backspace', 'Delete']} fitView fitViewOptions={{ padding: .2 }} minZoom={.15} maxZoom={1.6} nodesDraggable nodesConnectable elementsSelectable proOptions={{ hideAttribution: true }}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}
