import { Background, Controls, MarkerType, Position, ReactFlow, type Edge, type Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useI18n } from './i18n'
import type { WorkflowDefinition } from './types'
import { workflowEdgePresentation, workflowEdgeTypes, workflowGraphLayout, workflowNodeTypes } from './WorkflowGraphEdges'

export function WorkflowDefinitionMap({ definition, selectedTaskID, onSelect }: {
  definition: WorkflowDefinition; selectedTaskID: string | null; onSelect: (taskID: string) => void
}) {
  const { t } = useI18n()
  const { Tasks: tasks, Relations: relations, StartTaskIDs: starts } = definition.Graph
  const layout = workflowGraphLayout(tasks.map(task => task.ID), relations, starts)
  const columnGap = relations.some(relation => relation.Label?.trim()) ? 330 : 255
  const rows = new Map<number, number>()
  const nodes: Node[] = tasks.map(task => {
    const column = layout.depths.get(task.ID) ?? 0
    const row = rows.get(column) ?? 0
    rows.set(column, row + 1)
    return {
      id: task.ID,
      position: { x: column * columnGap, y: row * 112 },
      data: { label: <div className="definition-node-content"><span>{t(task.Executor)}</span><strong>{task.Title}</strong><small>{t(task.Execution === 'optional' ? 'optionalTask' : 'requiredTask')}</small></div> },
      type: 'workflowTask', className: `definition-flow-node ${selectedTaskID === task.ID ? 'selected' : ''}`,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })
  const taskIDs = new Set(tasks.map(task => task.ID))
  const edges: Edge[] = relations.filter(relation => taskIDs.has(relation.FromTaskID) && taskIDs.has(relation.ToTaskID)).map(relation => ({
    id: relation.ID, source: relation.FromTaskID, target: relation.ToTaskID,
    markerEnd: { type: MarkerType.ArrowClosed }, ...workflowEdgePresentation(relation.FromTaskID, relation.ToTaskID, layout.isBackEdge(relation.FromTaskID, relation.ToTaskID)),
    label: relation.Label || undefined,
    labelClassName: 'workflow-edge-label', labelBgPadding: [7, 4], labelBgBorderRadius: 5,
  }))
  return <div className="workflow-definition-graph"><ReactFlow nodes={nodes} edges={edges} edgeTypes={workflowEdgeTypes} nodeTypes={workflowNodeTypes} fitView fitViewOptions={{ padding: .22 }} minZoom={.15} maxZoom={1.5} nodesDraggable={false} nodesConnectable={false} elementsSelectable proOptions={{ hideAttribution: true }} onNodeClick={(_, node) => onSelect(node.id)}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}
